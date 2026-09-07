package trust

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// PersonaAPI es la dirección del servicio.
const PersonaAPI = "https://api.withpersona.com/api/v1"

// PersonaVersion fija la versión de la API. Se declara a propósito: sin ella,
// un cambio del proveedor podría alterar la forma de las respuestas sin aviso.
const PersonaVersion = "2023-01-05"

// PersonaConfig son los datos que hacen falta para hablar con Persona.
type PersonaConfig struct {
	// APIKey es la clave del panel de Persona.
	APIKey string
	// Plantillas asocia cada comprobación con la plantilla que la resuelve.
	// Una misma plantilla puede resolver varias: la de documento con selfie
	// acredita las dos cosas en un solo trámite.
	Plantillas map[CheckKind]string
	// WebhookSecret firma los avisos que envía Persona.
	WebhookSecret string
	// BaseURL permite apuntar a otro sitio en pruebas.
	BaseURL string
	// AceptarCompletado da por buena una verificación que termina en
	// "completed" sin decisión automática.
	//
	// Por defecto está desactivado, y es deliberado: una plantilla mal
	// configurada termina todo en "completed" sin haber comprobado nada, y
	// darlo por bueno acreditaría identidades que nadie ha revisado. Que la
	// gente se quede sin verificar se arregla; una identidad falsa dada por
	// buena, no.
	AceptarCompletado bool
	Cliente           *http.Client
}

// Persona verifica identidades contra el servicio de Persona.
type Persona struct {
	cfg PersonaConfig
}

var _ Provider = (*Persona)(nil)

// NewPersona construye el proveedor y comprueba que está bien configurado.
func NewPersona(cfg PersonaConfig) (*Persona, error) {
	if cfg.APIKey == "" {
		return nil, errors.New("falta la clave de API de Persona")
	}
	if len(cfg.Plantillas) == 0 {
		return nil, errors.New("hace falta al menos una plantilla de verificación")
	}
	for kind := range cfg.Plantillas {
		if !kind.Valid() {
			return nil, fmt.Errorf("%w: %s", ErrTipoDesconocido, kind)
		}
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = PersonaAPI
	}
	if cfg.Cliente == nil {
		cfg.Cliente = &http.Client{Timeout: 15 * time.Second}
	}
	return &Persona{cfg: cfg}, nil
}

// Start abre una verificación y devuelve el enlace al que mandar a la persona.
func (p *Persona) Start(ctx context.Context, userID string, kind CheckKind) (*Session, error) {
	plantilla, ok := p.cfg.Plantillas[kind]
	if !ok {
		return nil, fmt.Errorf("no hay plantilla configurada para %s", kind)
	}

	// reference-id ata la verificación con nuestro usuario, y vuelve en los
	// avisos: es lo que permite saber de quién es un veredicto sin guardar
	// nada más.
	cuerpo := map[string]any{
		"data": map[string]any{
			"attributes": map[string]any{
				"inquiry-template-id": plantilla,
				"reference-id":        userID,
			},
		},
	}

	var creada struct {
		Data struct {
			ID         string `json:"id"`
			Attributes struct {
				Status string `json:"status"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := p.llamar(ctx, http.MethodPost, "/inquiries", cuerpo, &creada); err != nil {
		return nil, err
	}
	if creada.Data.ID == "" {
		return nil, errors.New("Persona no devolvió identificador de verificación")
	}

	enlace, caduca, err := p.enlaceDeUnSoloUso(ctx, creada.Data.ID)
	if err != nil {
		return nil, err
	}
	return &Session{Ref: creada.Data.ID, RedirectURL: enlace, ExpiresAt: caduca}, nil
}

// enlaceDeUnSoloUso pide el enlace con el que la persona completa el trámite.
func (p *Persona) enlaceDeUnSoloUso(ctx context.Context, inquiryID string) (string, time.Time, error) {
	var resp struct {
		Meta struct {
			OneTimeLink          string `json:"one-time-link"`
			OneTimeLinkExpiresAt string `json:"one-time-link-expires-at"`
		} `json:"meta"`
	}
	ruta := "/inquiries/" + inquiryID + "/generate-one-time-link"
	if err := p.llamar(ctx, http.MethodPost, ruta, nil, &resp); err != nil {
		return "", time.Time{}, err
	}
	if resp.Meta.OneTimeLink == "" {
		return "", time.Time{}, errors.New("Persona no devolvió enlace de verificación")
	}

	caduca, err := time.Parse(time.RFC3339, resp.Meta.OneTimeLinkExpiresAt)
	if err != nil {
		// Persona los caduca en 24 h por defecto; si no lo dice, lo asumimos.
		caduca = time.Now().UTC().Add(24 * time.Hour)
	}
	return resp.Meta.OneTimeLink, caduca, nil
}

// Result consulta el veredicto. Se usa para reconciliar: el camino normal es el
// aviso que envía Persona, pero no se puede depender de que llegue.
func (p *Persona) Result(ctx context.Context, ref string) (*Outcome, error) {
	var resp struct {
		Data struct {
			Attributes struct {
				Status string `json:"status"`
			} `json:"attributes"`
		} `json:"data"`
		Included []struct {
			Type       string `json:"type"`
			Attributes struct {
				Status         string `json:"status"`
				ExpirationDate string `json:"expiration-date"`
			} `json:"attributes"`
		} `json:"included"`
	}
	if err := p.llamar(ctx, http.MethodGet, "/inquiries/"+ref+"?include=verifications", nil, &resp); err != nil {
		return nil, err
	}

	out := p.traducirEstado(resp.Data.Attributes.Status)

	// La caducidad del documento hace caducar sola la acreditación: sin esto,
	// un carné vencido seguiría contando como identidad verificada.
	for _, v := range resp.Included {
		if !strings.Contains(v.Type, "government-id") || v.Attributes.ExpirationDate == "" {
			continue
		}
		if f, err := time.Parse("2006-01-02", v.Attributes.ExpirationDate); err == nil {
			out.DocumentExpiresAt = f
		}
	}
	return &out, nil
}

// traducirEstado convierte el estado de Persona en nuestro veredicto.
func (p *Persona) traducirEstado(estado string) Outcome {
	switch estado {
	case "approved":
		return Outcome{Status: StatusVerified}
	case "completed":
		// Terminada pero sin decisión: solo vale si se ha configurado
		// expresamente que valga. Ver AceptarCompletado.
		if p.cfg.AceptarCompletado {
			return Outcome{Status: StatusVerified}
		}
		return Outcome{Status: StatusPending}
	case "declined":
		return Outcome{Status: StatusRejected, Reason: "la verificación no superó las comprobaciones"}
	case "failed":
		return Outcome{Status: StatusRejected, Reason: "la verificación no pudo completarse"}
	case "expired":
		return Outcome{Status: StatusExpired}
	default:
		// created, pending, needs_review y cualquier estado nuevo que añadan:
		// mejor seguir esperando que dar por buena una identidad.
		return Outcome{Status: StatusPending}
	}
}

// Cubre indica qué comprobaciones acredita una plantilla, según cuáles estén
// configuradas para usarla. Es lo que evita pedir dos veces el mismo trámite
// cuando una sola plantilla comprueba documento y cara.
func (p *Persona) Cubre(plantilla string) []CheckKind {
	var out []CheckKind
	for kind, id := range p.cfg.Plantillas {
		if id == plantilla {
			out = append(out, kind)
		}
	}
	return out
}

// --- Avisos entrantes ---

// ErrFirmaInvalida se devuelve cuando un aviso no viene firmado por Persona.
var ErrFirmaInvalida = errors.New("la firma del aviso no es válida")

// TolerenciaWebhook es lo viejo que puede ser un aviso antes de rechazarlo.
// Sin este límite, alguien que capturase un aviso legítimo podría reenviarlo
// indefinidamente.
const ToleranciaWebhook = 5 * time.Minute

// VerificarFirma comprueba que el aviso viene de Persona y es reciente.
//
// La cabecera es "t=<unix>,v1=<hmac>" y la firma se calcula sobre el texto
// "<t>.<cuerpo>". Puede traer varias firmas mientras se rota el secreto, así
// que basta con que una encaje.
func (p *Persona) VerificarFirma(cabecera string, cuerpo []byte, ahora time.Time) error {
	if p.cfg.WebhookSecret == "" {
		return errors.New("no hay secreto de webhook configurado")
	}

	var marca string
	var firmas []string
	for _, parte := range strings.Split(cabecera, ",") {
		clave, valor, ok := strings.Cut(strings.TrimSpace(parte), "=")
		if !ok {
			continue
		}
		switch clave {
		case "t":
			marca = valor
		case "v1":
			firmas = append(firmas, valor)
		}
	}
	if marca == "" || len(firmas) == 0 {
		return ErrFirmaInvalida
	}

	segundos, err := strconv.ParseInt(marca, 10, 64)
	if err != nil {
		return ErrFirmaInvalida
	}
	if desfase := ahora.Sub(time.Unix(segundos, 0)); desfase > ToleranciaWebhook || desfase < -ToleranciaWebhook {
		return fmt.Errorf("%w: el aviso llega con %s de desfase", ErrFirmaInvalida, desfase.Round(time.Second))
	}

	mac := hmac.New(sha256.New, []byte(p.cfg.WebhookSecret))
	mac.Write([]byte(marca + "."))
	mac.Write(cuerpo)
	esperada := mac.Sum(nil)

	for _, f := range firmas {
		recibida, err := hex.DecodeString(f)
		if err != nil {
			continue
		}
		// Comparación en tiempo constante: comparar con == filtraría el
		// secreto byte a byte a quien midiera los tiempos de respuesta.
		if hmac.Equal(recibida, esperada) {
			return nil
		}
	}
	return ErrFirmaInvalida
}

// AvisoPersona es lo que nos interesa de un aviso de Persona.
type AvisoPersona struct {
	// Evento es el nombre del suceso, por ejemplo "inquiry.approved".
	Evento string
	// Ref es el identificador de la verificación.
	Ref string
	// UsuarioRef es el reference-id con el que la abrimos.
	UsuarioRef string
	Outcome    Outcome
}

// LeerAviso interpreta el cuerpo de un aviso ya verificado.
func (p *Persona) LeerAviso(cuerpo []byte) (*AvisoPersona, error) {
	var sobre struct {
		Data struct {
			Attributes struct {
				Name    string `json:"name"`
				Payload struct {
					Data struct {
						ID         string `json:"id"`
						Attributes struct {
							Status      string `json:"status"`
							ReferenceID string `json:"reference-id"`
						} `json:"attributes"`
						Relationships struct {
							InquiryTemplate struct {
								Data struct {
									ID string `json:"id"`
								} `json:"data"`
							} `json:"inquiry-template"`
						} `json:"relationships"`
					} `json:"data"`
				} `json:"payload"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(cuerpo, &sobre); err != nil {
		return nil, fmt.Errorf("aviso ilegible: %w", err)
	}

	dentro := sobre.Data.Attributes.Payload.Data
	if dentro.ID == "" {
		return nil, errors.New("el aviso no identifica ninguna verificación")
	}

	out := p.traducirEstado(dentro.Attributes.Status)
	out.Cubre = p.Cubre(dentro.Relationships.InquiryTemplate.Data.ID)

	return &AvisoPersona{
		Evento:     sobre.Data.Attributes.Name,
		Ref:        dentro.ID,
		UsuarioRef: dentro.Attributes.ReferenceID,
		Outcome:    out,
	}, nil
}

// --- Transporte ---

func (p *Persona) llamar(ctx context.Context, metodo, ruta string, cuerpo any, destino any) error {
	var lector *bytes.Reader
	if cuerpo != nil {
		datos, err := json.Marshal(cuerpo)
		if err != nil {
			return err
		}
		lector = bytes.NewReader(datos)
	} else {
		lector = bytes.NewReader(nil)
	}

	req, err := http.NewRequestWithContext(ctx, metodo, p.cfg.BaseURL+ruta, lector)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Persona-Version", PersonaVersion)
	// Sin fijar la inflexión, la forma de las claves depende de la cuenta.
	req.Header.Set("Key-Inflection", "kebab")

	resp, err := p.cfg.Cliente.Do(req)
	if err != nil {
		return fmt.Errorf("consultando Persona: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Persona respondió %d en %s", resp.StatusCode, ruta)
	}
	if destino == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(destino); err != nil {
		return fmt.Errorf("respuesta de Persona ilegible: %w", err)
	}
	return nil
}
