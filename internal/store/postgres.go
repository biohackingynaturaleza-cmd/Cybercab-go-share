package store

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/domain"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/geo"
	"github.com/biohackingynaturaleza-cmd/cybercab-go-share/internal/trust"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Postgres guarda los datos en PostgreSQL con PostGIS.
//
// Las rutas son geography(LineString, 4326): PostGIS mide sobre el elipsoide y
// el índice GIST hace que buscar trayectos cercanos no recorra la tabla entera.
type Postgres struct {
	pool *pgxpool.Pool
}

var _ Store = (*Postgres)(nil)
var _ GeoSearcher = (*Postgres)(nil)

// NewPostgres abre el pool de conexiones y comprueba que responde.
func NewPostgres(ctx context.Context, dsn string) (*Postgres, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("configurando el pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("conectando con Postgres: %w", err)
	}
	return &Postgres{pool: pool}, nil
}

// Close libera las conexiones.
func (p *Postgres) Close() { p.pool.Close() }

// Migrate aplica las migraciones pendientes en orden y de una en una,
// registrando cada una para no repetirla.
func (p *Postgres) Migrate(ctx context.Context) error {
	if _, err := p.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			name       TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("preparando el registro de migraciones: %w", err)
	}

	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names) // el prefijo numérico fija el orden

	for _, name := range names {
		var applied bool
		if err := p.pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE name = $1)`, name).
			Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}

		sqlText, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		// Cada migración va en su transacción: o entra entera o no entra.
		tx, err := p.pool.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(sqlText)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("aplicando la migración %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO schema_migrations (name) VALUES ($1)`, name); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}

// --- Usuarios ---

func (p *Postgres) CreateUser(u *domain.User) error {
	_, err := p.pool.Exec(context.Background(), `
		INSERT INTO users (id, name, email, password_hash, idioma, rating, rating_count, ride_count, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		u.ID, u.Name, u.Email, u.PasswordHash, domain.NormalizarIdioma(u.Idioma),
		u.Rating, u.RatingCount, u.RideCount, u.CreatedAt)
	if esViolacionUnica(err, "users_email_key") {
		return ErrEmailEnUso
	}
	return err
}

const selectUser = `SELECT id, name, email, password_hash, idioma, rating, rating_count, ride_count, created_at FROM users`

func (p *Postgres) GetUser(id string) (*domain.User, error) {
	return p.scanUser(p.pool.QueryRow(context.Background(), selectUser+` WHERE id = $1`, id))
}

func (p *Postgres) GetUserByEmail(email string) (*domain.User, error) {
	return p.scanUser(p.pool.QueryRow(context.Background(),
		selectUser+` WHERE lower(email) = lower($1)`, strings.TrimSpace(email)))
}

func (p *Postgres) scanUser(row pgx.Row) (*domain.User, error) {
	var u domain.User
	err := row.Scan(&u.ID, &u.Name, &u.Email, &u.PasswordHash, &u.Idioma, &u.Rating, &u.RatingCount, &u.RideCount, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// --- Trayectos ---

func (p *Postgres) CreateTrip(t *domain.Trip) error {
	_, err := p.pool.Exec(context.Background(), `
		INSERT INTO trips (
			id, host_id, origin_name, destination_name, route, duration_min,
			route_source, departure_time, vehicle, seats_total, seats_taken,
			max_detour_km, min_trust_level, notes, status, created_at,
			fleet_ride_ref, tarifa_declarada_cents, tarifa_real_cents
		) VALUES (
			$1, $2, $3, $4, ST_GeogFromText($5), $6,
			$7, $8, $9, $10, $11,
			$12, $13, $14, $15, $16,
			$17, $18, $19
		)`,
		t.ID, t.HostID, t.Origin.Name, t.Destination.Name, lineStringWKT(t.Route), t.DurationMin,
		t.RouteSource, t.DepartureTime, string(t.Vehicle), t.SeatsTotal, t.SeatsTaken,
		t.MaxDetourKm, t.MinTrustLevel.Label(), t.Notes, string(t.Status), t.CreatedAt,
		t.FleetRideRef, t.TarifaDeclaradaCents, t.TarifaRealCents)
	if esViolacionUnica(err, "trips_fleet_ride_ref") {
		return ErrReferenciaDeFlotaEnUso
	}
	return err
}

// ErrReferenciaDeFlotaEnUso se devuelve al registrar una referencia de viaje
// que ya está en otro trayecto: sería el mismo viaje cobrado dos veces.
var ErrReferenciaDeFlotaEnUso = errors.New("esa referencia de viaje ya está registrada en otro trayecto")

// selectTrip devuelve la ruta como GeoJSON, que es directo de leer en Go.
const selectTrip = `
	SELECT id, host_id, origin_name, destination_name, ST_AsGeoJSON(route),
	       duration_min, route_source, departure_time, vehicle, seats_total,
	       seats_taken, max_detour_km, min_trust_level, notes, status, created_at,
	       fleet_ride_ref, tarifa_declarada_cents, tarifa_real_cents
	FROM trips`

func (p *Postgres) GetTrip(id string) (*domain.Trip, error) {
	rows, err := p.pool.Query(context.Background(), selectTrip+` WHERE id = $1`, id)
	if err != nil {
		return nil, err
	}
	trips, err := scanTrips(rows)
	if err != nil {
		return nil, err
	}
	if len(trips) == 0 {
		return nil, ErrNotFound
	}
	return trips[0], nil
}

func (p *Postgres) UpdateTrip(t *domain.Trip) error {
	tag, err := p.pool.Exec(context.Background(), `
		UPDATE trips SET
			origin_name = $2, destination_name = $3, route = ST_GeogFromText($4),
			duration_min = $5, route_source = $6, departure_time = $7,
			vehicle = $8, seats_total = $9, seats_taken = $10,
			max_detour_km = $11, min_trust_level = $12, notes = $13, status = $14,
			fleet_ride_ref = $15, tarifa_declarada_cents = $16, tarifa_real_cents = $17
		WHERE id = $1`,
		t.ID, t.Origin.Name, t.Destination.Name, lineStringWKT(t.Route),
		t.DurationMin, t.RouteSource, t.DepartureTime,
		string(t.Vehicle), t.SeatsTotal, t.SeatsTaken,
		t.MaxDetourKm, t.MinTrustLevel.Label(), t.Notes, string(t.Status), t.FleetRideRef,
		t.TarifaDeclaradaCents, t.TarifaRealCents)
	if esViolacionUnica(err, "trips_fleet_ride_ref") {
		return ErrReferenciaDeFlotaEnUso
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) ListOpenTrips() ([]*domain.Trip, error) {
	rows, err := p.pool.Query(context.Background(),
		selectTrip+` WHERE status = 'open' ORDER BY departure_time`)
	if err != nil {
		return nil, err
	}
	return scanTrips(rows)
}

// ReserveSeats ocupa plazas en una única sentencia. La condición del WHERE es
// lo que garantiza que dos peticiones simultáneas no se lleven la misma plaza:
// la segunda no encuentra fila que actualizar.
func (p *Postgres) TripsByHost(hostID string) ([]*domain.Trip, error) {
	rows, err := p.pool.Query(context.Background(),
		selectTrip+` WHERE host_id = $1 ORDER BY departure_time DESC`, hostID)
	if err != nil {
		return nil, err
	}
	return scanTrips(rows)
}

func (p *Postgres) ReserveSeats(tripID string, seats int) error {
	tag, err := p.pool.Exec(context.Background(), `
		UPDATE trips SET
			seats_taken = seats_taken + $2,
			status = CASE WHEN seats_total - (seats_taken + $2) = 0 THEN 'full' ELSE status END
		WHERE id = $1
		  AND status = 'open'
		  AND seats_total - seats_taken >= $2`,
		tripID, seats)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		// O no existe, o no quedan plazas. Distinguirlo cuesta otra consulta y
		// solo importa para el mensaje de error.
		if _, err := p.GetTrip(tripID); err != nil {
			return err
		}
		return ErrSinPlazas
	}
	return nil
}

func (p *Postgres) ReleaseSeats(tripID string, seats int) error {
	tag, err := p.pool.Exec(context.Background(), `
		UPDATE trips SET
			seats_taken = GREATEST(seats_taken - $2, 0),
			status = CASE WHEN status = 'full' THEN 'open' ELSE status END
		WHERE id = $1`,
		tripID, seats)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Reservas ---

func (p *Postgres) CreateBooking(b *domain.Booking) error {
	_, err := p.pool.Exec(context.Background(), `
		INSERT INTO bookings (
			id, trip_id, passenger_id, pickup_name, pickup_point,
			dropoff_name, dropoff_point, pickup_along_km, dropoff_along_km,
			seats, price_cents, status, created_at
		) VALUES (
			$1, $2, $3, $4, ST_GeogFromText($5),
			$6, ST_GeogFromText($7), $8, $9,
			$10, $11, $12, $13
		)`,
		b.ID, b.TripID, b.PassengerID, b.Pickup.Name, pointWKT(b.Pickup.Point),
		b.Dropoff.Name, pointWKT(b.Dropoff.Point), b.PickupAlongKm, b.DropoffAlongKm,
		b.Seats, b.PriceCents, string(b.Status), b.CreatedAt)
	if esViolacionUnica(err, "bookings_una_viva_por_pasajero") {
		return ErrReservaDuplicada
	}
	return err
}

// ErrReservaDuplicada se devuelve si alguien intenta reservar dos veces en el
// mismo trayecto teniendo ya una reserva viva.
var ErrReservaDuplicada = errors.New("ya tienes una reserva en este trayecto")

const selectBooking = `
	SELECT id, trip_id, passenger_id, pickup_name, ST_AsGeoJSON(pickup_point),
	       dropoff_name, ST_AsGeoJSON(dropoff_point), pickup_along_km,
	       dropoff_along_km, seats, price_cents, status, created_at
	FROM bookings`

func (p *Postgres) GetBooking(id string) (*domain.Booking, error) {
	rows, err := p.pool.Query(context.Background(), selectBooking+` WHERE id = $1`, id)
	if err != nil {
		return nil, err
	}
	bookings, err := scanBookings(rows)
	if err != nil {
		return nil, err
	}
	if len(bookings) == 0 {
		return nil, ErrNotFound
	}
	return bookings[0], nil
}

func (p *Postgres) UpdateBooking(b *domain.Booking) error {
	tag, err := p.pool.Exec(context.Background(), `
		UPDATE bookings SET seats = $2, price_cents = $3, status = $4 WHERE id = $1`,
		b.ID, b.Seats, b.PriceCents, string(b.Status))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) BookingsByTrip(tripID string) ([]*domain.Booking, error) {
	rows, err := p.pool.Query(context.Background(),
		selectBooking+` WHERE trip_id = $1 ORDER BY created_at, id`, tripID)
	if err != nil {
		return nil, err
	}
	return scanBookings(rows)
}

func (p *Postgres) BookingsByPassenger(userID string) ([]*domain.Booking, error) {
	rows, err := p.pool.Query(context.Background(),
		selectBooking+` WHERE passenger_id = $1 ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, err
	}
	return scanBookings(rows)
}

// --- Búsqueda geográfica ---

// CandidateTrips deja en Postgres el trabajo pesado: descarta con el índice
// espacial todo trayecto cuya ruta no pase cerca de la recogida y de la bajada.
// El orden exacto sobre la ruta lo decide después el paquete matching.
func (p *Postgres) CandidateTrips(ctx context.Context, q GeoQuery) ([]*domain.Trip, error) {
	seats := q.Seats
	if seats < 1 {
		seats = 1
	}
	radiusM := q.MaxDistanceKm * 1000
	if radiusM <= 0 {
		radiusM = 1500
	}

	rows, err := p.pool.Query(ctx, selectTrip+`
		WHERE status = 'open'
		  AND seats_total - seats_taken >= $1
		  AND ($2::timestamptz IS NULL OR departure_time >= $2)
		  AND ($3::timestamptz IS NULL OR departure_time <= $3)
		  AND ST_DWithin(route, ST_GeogFromText($4), $6)
		  AND ST_DWithin(route, ST_GeogFromText($5), $6)
		ORDER BY departure_time`,
		seats, nullTime(q.EarliestDeparture), nullTime(q.LatestDeparture),
		pointWKT(q.Pickup), pointWKT(q.Dropoff), radiusM)
	if err != nil {
		return nil, err
	}
	return scanTrips(rows)
}

// --- Conversión con PostGIS ---

// lineStringWKT convierte una ruta al texto que entiende PostGIS.
// Ojo al orden: WKT es "longitud latitud", al revés de como se nombran.
func lineStringWKT(r geo.Route) string {
	var b strings.Builder
	b.WriteString("SRID=4326;LINESTRING(")
	for i, p := range r {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(coord(p.Lng))
		b.WriteByte(' ')
		b.WriteString(coord(p.Lat))
	}
	b.WriteByte(')')
	return b.String()
}

func pointWKT(p geo.Point) string {
	return "SRID=4326;POINT(" + coord(p.Lng) + " " + coord(p.Lat) + ")"
}

func coord(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// geoJSONGeometry es lo que devuelve ST_AsGeoJSON.
type geoJSONGeometry struct {
	Type        string          `json:"type"`
	Coordinates json.RawMessage `json:"coordinates"`
}

// parseLineString lee una polilínea de GeoJSON, invirtiendo el orden
// [lng, lat] al que usa el dominio.
func parseLineString(raw string) (geo.Route, error) {
	var g geoJSONGeometry
	if err := json.Unmarshal([]byte(raw), &g); err != nil {
		return nil, fmt.Errorf("leyendo la ruta: %w", err)
	}
	var coords [][2]float64
	if err := json.Unmarshal(g.Coordinates, &coords); err != nil {
		return nil, fmt.Errorf("leyendo las coordenadas de la ruta: %w", err)
	}
	route := make(geo.Route, 0, len(coords))
	for _, c := range coords {
		route = append(route, geo.Point{Lat: c[1], Lng: c[0]})
	}
	return route, nil
}

func parsePoint(raw string) (geo.Point, error) {
	var g geoJSONGeometry
	if err := json.Unmarshal([]byte(raw), &g); err != nil {
		return geo.Point{}, err
	}
	var c [2]float64
	if err := json.Unmarshal(g.Coordinates, &c); err != nil {
		return geo.Point{}, err
	}
	return geo.Point{Lat: c[1], Lng: c[0]}, nil
}

func scanTrips(rows pgx.Rows) ([]*domain.Trip, error) {
	defer rows.Close()
	var out []*domain.Trip
	for rows.Next() {
		var (
			t          domain.Trip
			routeJSON  string
			vehicle    string
			status     string
			originName string
			destName   string
			trustLevel string
		)
		if err := rows.Scan(&t.ID, &t.HostID, &originName, &destName, &routeJSON,
			&t.DurationMin, &t.RouteSource, &t.DepartureTime, &vehicle, &t.SeatsTotal,
			&t.SeatsTaken, &t.MaxDetourKm, &trustLevel, &t.Notes, &status, &t.CreatedAt,
			&t.FleetRideRef, &t.TarifaDeclaradaCents, &t.TarifaRealCents); err != nil {
			return nil, err
		}
		nivel, ok := trust.ParseLevel(trustLevel)
		if !ok {
			return nil, fmt.Errorf("nivel de confianza desconocido en el trayecto %s: %q", t.ID, trustLevel)
		}
		t.MinTrustLevel = nivel
		route, err := parseLineString(routeJSON)
		if err != nil {
			return nil, err
		}
		t.Route = route
		t.Vehicle = domain.VehicleType(vehicle)
		t.Status = domain.TripStatus(status)
		// Origen y destino son los extremos de la ruta: así no pueden
		// contradecirla.
		t.Origin = domain.Place{Name: originName, Point: route[0]}
		t.Destination = domain.Place{Name: destName, Point: route[len(route)-1]}
		t.DepartureTime = t.DepartureTime.UTC()
		t.CreatedAt = t.CreatedAt.UTC()
		out = append(out, &t)
	}
	return out, rows.Err()
}

func scanBookings(rows pgx.Rows) ([]*domain.Booking, error) {
	defer rows.Close()
	var out []*domain.Booking
	for rows.Next() {
		var (
			b           domain.Booking
			pickupJSON  string
			dropoffJSON string
			pickupName  string
			dropoffName string
			status      string
		)
		if err := rows.Scan(&b.ID, &b.TripID, &b.PassengerID, &pickupName, &pickupJSON,
			&dropoffName, &dropoffJSON, &b.PickupAlongKm, &b.DropoffAlongKm,
			&b.Seats, &b.PriceCents, &status, &b.CreatedAt); err != nil {
			return nil, err
		}
		pickup, err := parsePoint(pickupJSON)
		if err != nil {
			return nil, err
		}
		dropoff, err := parsePoint(dropoffJSON)
		if err != nil {
			return nil, err
		}
		b.Pickup = domain.Place{Name: pickupName, Point: pickup}
		b.Dropoff = domain.Place{Name: dropoffName, Point: dropoff}
		b.Status = domain.BookingStatus(status)
		b.CreatedAt = b.CreatedAt.UTC()
		out = append(out, &b)
	}
	return out, rows.Err()
}

func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// esViolacionUnica identifica el choque contra un índice único concreto.
func esViolacionUnica(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) &&
		pgErr.Code == "23505" &&
		strings.Contains(pgErr.ConstraintName, constraint)
}

// TruncateAll vacía todas las tablas de datos. Solo para pruebas: deja el
// esquema intacto pero borra su contenido.
func (p *Postgres) TruncateAll(ctx context.Context) error {
	_, err := p.pool.Exec(ctx,
		`TRUNCATE user_blocks, identity_checks, bookings, trips, users RESTART IDENTITY CASCADE`)
	return err
}
