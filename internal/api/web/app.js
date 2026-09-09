'use strict';

/* Cybercab Go Share — interfaz.
 *
 * Sin dependencias externas a propósito: la política de seguridad solo permite
 * scripts de este mismo servidor, así que no hay CDN que pueda caerse, cambiar
 * bajo los pies o rastrear a quien entra.
 *
 * El mapa es esquemático y no de calles porque lo que hay que enseñar es si dos
 * trayectos se solapan y cuánto. De navegar ya se encarga el robotaxi. */

const $ = (id) => document.getElementById(id);
const ANIM = !window.matchMedia('(prefers-reduced-motion: reduce)').matches;

const estado = {
  token: localStorage.getItem('token') || '',
  usuario: null,
  perfil: null,
  resumen: null,
  verificaciones: [],
  config: null,
  zonas: [],
  modo: 'buscar',
  resultados: [],
  pendientes: [],
  contactos: [],
  saldo: null,
  maxContactos: 3,
  activos: [],
  seleccionado: null,
  mios: [],
  incidencias: [],
  buscando: false,
};

/* ============ Cliente de la API ============ */

async function api(metodo, ruta, cuerpo) {
  const cab = { 'Content-Type': 'application/json' };
  if (estado.token) cab.Authorization = 'Bearer ' + estado.token;

  const resp = await fetch(ruta, {
    method: metodo, headers: cab,
    body: cuerpo === undefined ? undefined : JSON.stringify(cuerpo),
  });

  let datos = null;
  try { datos = await resp.json(); } catch { /* sin cuerpo */ }

  if (!resp.ok) {
    const err = new Error((datos && datos.error) || `Error ${resp.status}`);
    err.status = resp.status;
    err.datos = datos || {};
    throw err;
  }
  return datos;
}

/* ============ Notificaciones ============ */

function toast(titulo, detalle = '', tipo = '') {
  const el = document.createElement('div');
  el.className = 'toast ' + tipo;
  el.innerHTML = `<strong>${escapar(titulo)}</strong>${detalle}`;
  $('avisos').appendChild(el);

  const cerrar = () => {
    el.classList.add('se-va');
    el.addEventListener('animationend', () => el.remove(), { once: true });
    // Red de seguridad: en una pestaña de fondo el navegador frena las
    // animaciones y ese evento puede no llegar nunca, dejando avisos viejos
    // apilados encima de los nuevos.
    setTimeout(() => el.remove(), 1000);
  };
  const t = setTimeout(cerrar, tipo === 'error' ? 8000 : 5000);
  el.addEventListener('click', () => { clearTimeout(t); cerrar(); });
}

function escapar(s) {
  return String(s ?? '').replace(/[&<>"']/g, (c) =>
    ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

/* Un fallo de confianza se traduce en algo accionable: decir "no puedes" sin
   decir qué hacer solo consigue que la gente se vaya. */
function parrafo(txt) { return `<p style="margin:6px 0 0">${escapar(txt)}</p>`; }

/* Un fallo de confianza se traduce en algo accionable, y se compone aquí en vez
   de enseñar el texto del servidor: así la explicación sale en el idioma de
   quien la lee. */
function explicarError(e) {
  if (e.status === 403 && e.datos.te_falta) {
    const faltan = e.datos.te_falta.map(nombreComprobacion).join(', ');
    toast(t('aviso.nopuedes.t'),
      parrafo(e.datos.exigido ? textoSuelo(e.datos.exigido === 'verificado' ? 2 : 4) : '') +
      parrafo(t('aviso.tefalta', { lista: faltan })), 'error');
    return;
  }
  toast(t('aviso.fallo.t'), parrafo(e.message), 'error');
}

/* textoSuelo se compone en la interfaz a partir del aforo, no se copia del
   servidor: el servidor devuelve datos, los idiomas los pone la interfaz. */
function textoSuelo(plazas) {
  return plazas <= 2 ? t('suelo.biplaza') : t('suelo.amplio', { n: plazas });
}

/* ============ Precios ============ */

/* Mismo cálculo que el servidor. Se usa solo para enseñar estimaciones antes de
   pedir nada; el importe que se cobra siempre lo decide el servidor. */
function costeEstimado(km) {
  const t = estado.config?.tarifa;
  if (!t) return 0;
  const min = km / (estado.config.velocidad_media_kmh || 45) * 60;
  const total = t.base_cents + km * t.por_km_cents + min * t.por_minuto_cents;
  return Math.max(t.minimo_cents, Math.round(total));
}

const R_TIERRA = 6371.0088;
function distanciaKm(a, b) {
  const rad = (g) => g * Math.PI / 180;
  const dLat = rad(b.lat - a.lat), dLng = rad(b.lng - a.lng);
  const h = Math.sin(dLat / 2) ** 2 +
    Math.cos(rad(a.lat)) * Math.cos(rad(b.lat)) * Math.sin(dLng / 2) ** 2;
  return 2 * R_TIERRA * Math.asin(Math.min(1, Math.sqrt(h)));
}

/* Dólares con el formato de cada idioma. Se compone a mano porque el formato
   automático en español produce "5,41 US$", que nadie escribe así. */
function euros(cents) {
  const n = (cents / 100).toLocaleString(idioma(), {
    minimumFractionDigits: 2, maximumFractionDigits: 2,
  });
  return idioma() === 'es' ? `${n} $` : `$${n}`;
}

function fecha(d) {
  return d.toLocaleString(idioma(),
    { weekday: 'short', day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' });
}

/* ============ Sesión ============ */

let esAlta = true;

function mostrarAcceso(alta) {
  esAlta = alta;
  $('portada').classList.add('oculto');
  $('recuperar').classList.add('oculto');
  $('acceso').classList.remove('oculto');
  $('acceso-titulo').textContent = t(alta ? 'acceso.crear' : 'acceso.entrar');
  $('acceso-sub').textContent = t(alta ? 'acceso.sub.crear' : 'acceso.sub.entrar');
  $('campo-nombre').classList.toggle('oculto', !alta);
  $('btn-acceso').textContent = t(alta ? 'acceso.crear' : 'acceso.entrar');
  $('clave').autocomplete = alta ? 'new-password' : 'current-password';
  $('cambiar-texto').textContent = t(alta ? 'acceso.ya' : 'acceso.aun');
  $('cambiar-modo').textContent = t(alta ? 'acceso.entrar.enlace' : 'acceso.crear.enlace');

  // La casilla solo aparece al crear cuenta: quien ya la tiene, ya aceptó.
  $('campo-terminos').classList.toggle('oculto', !alta);
  $('texto-terminos').innerHTML = textoDeAceptacion();
  // Y se desmarca cada vez que se abre el formulario: si quedara marcada de
  // una visita anterior, la aceptación no sería un acto de nadie.
  $('acepta-terminos').checked = false;
  // Recuperar la contraseña solo tiene sentido para quien ya tiene cuenta.
  $('olvide').classList.toggle('oculto', alta);

  (alta ? $('nombre') : $('email')).focus();
}

/* textoDeAceptacion compone la frase con los dos enlaces dentro. Se arma aquí y
   no en el HTML porque el orden de las palabras cambia con el idioma, y con él
   el sitio donde caen los enlaces. El texto se escapa antes de meter los
   enlaces: lo único que entra como HTML es lo que ponemos nosotros. */
function textoDeAceptacion() {
  const enlace = (href, clave) =>
    `<a href="${href}" target="_blank" rel="noopener">${escapar(t(clave))}</a>`;
  return escapar(t('acceso.terminos'))
    .replace('{terminos}', enlace('/terminos.html', 'legal.terminos'))
    .replace('{privacidad}', enlace('/privacidad.html', 'legal.privacidad'));
}

function volverAPortada() {
  $('acceso').classList.add('oculto');
  $('recuperar').classList.add('oculto');
  $('portada').classList.remove('oculto');
  if (location.hash.startsWith('#recuperar=')) history.replaceState(null, '', location.pathname);
}

async function enviarAcceso(ev) {
  ev.preventDefault();
  const email = $('email').value.trim();
  const clave = $('clave').value;
  const nombre = $('nombre').value.trim();

  if (esAlta && !nombre) {
    toast(t('aviso.nombre.t'), parrafo(t('aviso.nombre.d')), 'error');
    $('nombre').focus();
    return;
  }
  if (esAlta && !$('acepta-terminos').checked) {
    toast(t('aviso.terminos.t'), parrafo(t('aviso.terminos.d')), 'error');
    $('acepta-terminos').focus();
    return;
  }

  const btn = $('btn-acceso');
  const antes = btn.textContent;
  btn.disabled = true;
  btn.textContent = t(esAlta ? 'acceso.creando' : 'acceso.entrando');
  try {
    const sesion = esAlta
      // El idioma se manda al registrarse: es en el que se le escribirá
      // después. Sin esto, quien usa la app en español recibía los correos en
      // inglés.
      ? await api('POST', '/api/v1/auth/register',
          { name: nombre, email, password: clave, idioma: idioma(), acepta_terminos: true })
      : await api('POST', '/api/v1/auth/login', { email, password: clave });

    estado.token = sesion.token;
    estado.usuario = sesion.user;
    localStorage.setItem('token', estado.token);
    if (esAlta) toast(t('aviso.cuenta.t'), parrafo(t('aviso.cuenta.d')), 'bien');
    await entrarEnLaApp();
  } catch (e) {
    if (!avisarSiCortaron(e)) toast(t('aviso.fallo.t'), parrafo(mensajeDeError(e)), 'error');
  } finally {
    btn.disabled = false;
    btn.textContent = antes;
  }
}

/* mensajeDeError elige el texto que lee la persona. Cuando el servidor manda un
   código estable, el texto lo pone la interfaz en su idioma; el mensaje del
   servidor es el último recurso. */
function mensajeDeError(e) {
  switch (e.datos?.codigo) {
    case 'enlace_no_valido': return t('rec.caducado');
    case 'codigo_agotado': return t('cod.agotado');
    case 'codigo_invalido': return t('cod.malo', { n: e.datos.intentos_restantes ?? 0 });
    default: return e.message;
  }
}

/* avisarSiCortaron traduce el 429 a algo con el que la persona sabe qué hacer.
   Un "Error 429" en crudo no le dice a nadie que basta con esperar. */
function avisarSiCortaron(e) {
  if (e.status !== 429) return false;
  toast(t('aviso.demasiados.t'),
    parrafo(t('aviso.demasiados.d', { segundos: e.datos.reintentar_en_s || 60 })), 'error');
  return true;
}

async function entrarEnLaApp() {
  $('acceso').classList.add('oculto');
  $('portada').classList.add('oculto');
  $('panel').hidden = false;
  await refrescar();
  pintar();
  dibujarMapa();
}

function salir() {
  estado.token = '';
  estado.usuario = estado.perfil = estado.resumen = null;
  estado.resultados = []; estado.mios = []; estado.verificaciones = [];
  estado.seleccionado = null;
  localStorage.removeItem('token');
  $('panel').hidden = true;
  volverAPortada();
  pintar();
  dibujarMapa();
}

async function cargarPendientesDeValorar() {
  try {
    const r = await api('GET', '/api/v1/me/valoraciones/pendientes');
    estado.pendientes = r.pendientes || [];
  } catch {
    estado.pendientes = [];
  }
}

async function refrescar() {
  if (!estado.usuario) return;
  const [perfil, verif, resumen] = await Promise.all([
    api('GET', `/api/v1/users/${estado.usuario.id}/confianza`),
    api('GET', '/api/v1/me/verificaciones'),
    api('GET', '/api/v1/me/resumen'),
  ]);
  estado.perfil = perfil;
  estado.verificaciones = verif.verificaciones || [];
  estado.resumen = resumen;
  await Promise.all([
    cargarMisTrayectos(), cargarIncidencias(), cargarPendientesDeValorar(),
    cargarContactos(), cargarViajesActivos(), cargarSaldo(),
  ]);
}

/* ============ Confianza ============ */

const COMPROBACIONES = ['email', 'phone', 'government_id', 'selfie_liveness'];
const DESBLOQUEA = { phone: 'check.phone.d', selfie_liveness: 'check.selfie_liveness.d' };

function nombreComprobacion(id) { return t('check.' + id).toLowerCase(); }

function hecha(id) {
  return estado.verificaciones.some((v) => v.kind === id && v.status === 'verified');
}

/* correoAbierto guarda si la fila del correo está enseñando el campo del
   código. Es estado de la vista, no del servidor: por eso vive aquí. */
let correoAbierto = false;

async function verificar(id, boton) {
  // El buzón no va al proveedor de identidad: el código se teclea aquí mismo.
  if (id === 'email') {
    correoAbierto = true;
    pintarConfianza();
    $('codigo-correo')?.focus();
    return;
  }
  boton.disabled = true;
  boton.textContent = '…';
  try {
    const r = await api('POST', '/api/v1/me/verificaciones', { kind: id });
    const ref = r.verificacion.provider_ref;
    try {
      // En desarrollo hay un atajo. En producción esto no existe y hay que
      // completar el trámite en la página del proveedor.
      await api('POST', `/api/v1/dev/verificaciones/${ref}/resolver`, { verificar: true });
    } catch {
      window.open(r.continuar_en, '_blank', 'noopener');
      toast(t('aviso.completa.t'), parrafo(t('aviso.completa.d')));
    }
    await refrescar();
    const nivelAntes = estado.perfil?.nivel;
    pintar();
    if (nivelAntes === 'verificado') {
      toast(t('aviso.verificado.t'), parrafo(t('aviso.verificado.d')), 'bien');
    }
  } catch (e) {
    explicarError(e);
    pintar();
  }
}

function pintarConfianza() {
  const nivel = estado.perfil?.nivel || 'nuevo';
  $('mi-nivel').textContent = t('nivel.' + nivel);
  $('mi-nivel').className = 'nivel nivel-' + claseNivel(nivel);
  // La media va junto al nivel porque son lo mismo: las dos cosas que otra
  // persona mira antes de decidir si se sube a un coche contigo.
  $('mi-nota').innerHTML = nota(estado.perfil?.estadisticas);

  const total = COMPROBACIONES.length;
  const listas = COMPROBACIONES.filter(hecha).length;
  $('progreso-barra').style.width = Math.round(listas / total * 100) + '%';

  const verificado = nivel === 'verificado' || nivel === 'veterano';
  $('confianza-texto').textContent = verificado
    ? t('confianza.hecho')
    : t('confianza.falta', { n: total - listas, total });

  $('lista-comprobaciones').innerHTML = COMPROBACIONES.map((c) => {
    const ok = hecha(c);
    return `<li class="${ok ? 'hecha' : ''}">
      <span>${escapar(t('check.' + c))}
        ${!ok && DESBLOQUEA[c] ? `<span class="desbloquea">${escapar(t(DESBLOQUEA[c]))}</span>` : ''}</span>
      ${ok ? `<span class="ok" aria-label="${escapar(t('nivel.verificado'))}">✓</span>`
           : `<button class="btn-2 btn-fino" data-verificar="${c}">${escapar(t('confianza.verificar'))}</button>`}
    </li>${c === 'email' && !ok && correoAbierto ? campoDelCodigo() : ''}`;
  }).join('');

  $('lista-comprobaciones').querySelectorAll('[data-verificar]').forEach((b) =>
    b.addEventListener('click', () => verificar(b.dataset.verificar, b)));
  conectarCodigoDeCorreo();
}

/* ============ Código del buzón ============ */

/* El código se teclea aquí, debajo de su propia fila: mandar a alguien a otra
   pantalla para escribir seis dígitos que acaba de leer en el móvil es perder
   por el camino a la mitad de la gente. */
function campoDelCodigo() {
  return `<li class="codigo-fila">
    <div style="width:100%">
      <p class="tenue" style="margin:0 0 8px">${escapar(t('cod.explica'))}</p>
      <div class="codigo-caja">
        <input id="codigo-correo" inputmode="numeric" autocomplete="one-time-code"
               pattern="[0-9]*" placeholder="······"
               aria-label="${escapar(t('cod.campo'))}">
        <button class="btn-1 btn-fino" id="codigo-enviar">${escapar(t('cod.confirmar'))}</button>
      </div>
      <button class="enlace" id="codigo-reenviar" style="margin-top:8px;font-size:12px">${escapar(t('cod.reenviar'))}</button>
    </div>
  </li>`;
}

function conectarCodigoDeCorreo() {
  const campo = $('codigo-correo');
  if (!campo) return;

  // Solo dígitos, y seis. El recorte lo hace esto y no un maxlength en el
  // campo: maxlength corta antes de que se limpien los espacios, así que pegar
  // "447 363" —que es como lo copia media gente— dejaba "44736".
  campo.addEventListener('input', () => {
    campo.value = campo.value.replace(/\D/g, '').slice(0, 6);
  });
  campo.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') { e.preventDefault(); enviarCodigo(); }
  });
  $('codigo-enviar').addEventListener('click', enviarCodigo);
  $('codigo-reenviar').addEventListener('click', reenviarCodigo);
}

async function enviarCodigo() {
  const campo = $('codigo-correo');
  const boton = $('codigo-enviar');
  if (campo.value.length !== 6) {
    toast(t('cod.corto.t'), parrafo(t('cod.corto.d')), 'error');
    campo.focus();
    return;
  }
  const antes = boton.textContent;
  boton.disabled = true;
  boton.textContent = '…';
  try {
    await api('POST', '/api/v1/me/correo/confirmar', { codigo: campo.value });
    correoAbierto = false;
    toast(t('cod.hecho.t'), parrafo(t('cod.hecho.d')), 'bien');
    await refrescar();
    pintar();
  } catch (e) {
    toast(t('aviso.fallo.t'), parrafo(mensajeDeError(e)), 'error');
    boton.disabled = false;
    boton.textContent = antes;
    campo.select();
  }
}

async function reenviarCodigo() {
  const boton = $('codigo-reenviar');
  boton.disabled = true;
  try {
    await api('POST', '/api/v1/me/correo/reenviar', {});
    toast(t('cod.reenviado.t'), parrafo(t('cod.reenviado.d')), 'bien');
  } catch (e) {
    if (!avisarSiCortaron(e)) toast(t('aviso.fallo.t'), parrafo(mensajeDeError(e)), 'error');
  } finally {
    boton.disabled = false;
  }
}

/* ============ Ahorro ============ */

function pintarAhorro() {
  const r = estado.resumen;

  // El aviso de peticiones va antes que nada y con su propia condición: quien
  // solo organiza viajes no tiene ahorro todavía, y ocultarle el aviso junto
  // con la caja del ahorro le escondía justo lo único que tiene que hacer.
  const pend = $('pendientes');
  pend.classList.toggle('oculto', !r || !r.peticiones_por_responder);
  if (r) {
    $('pendientes-num').textContent = r.peticiones_por_responder;
    pend.setAttribute('aria-label',
      `${r.peticiones_por_responder} · ${t('pendientes.texto')}`);
  }

  const caja = $('caja-ahorro');
  const hayAlgo = r && (r.viajes_compartidos || r.viajes_pendientes);
  caja.classList.toggle('oculto', !hayAlgo);
  if (!hayAlgo) return;

  // Mientras no se haya viajado todavía no hay ahorro, pero sí una previsión.
  // Enseñar "0,00 $" a quien acaba de reservar su primer viaje es desanimarle
  // justo cuando más ilusión tiene.
  const yaViajado = r.viajes_compartidos > 0;
  $('ahorro-cifra').textContent = euros(yaViajado ? r.ahorro_cents : r.ahorro_previsto_cents);

  const partes = [];
  if (r.viajes_compartidos) partes.push(t('ahorro.viajes', { n: r.viajes_compartidos }));
  if (r.km_compartidos >= 1) partes.push(`${r.km_compartidos.toFixed(0)} km`);
  if (r.viajes_pendientes) partes.push(t('ahorro.pordelante', { n: r.viajes_pendientes }));

  $('ahorro-pie').textContent = t(yaViajado ? 'ahorro.llevas' : 'ahorro.vas') +
    (partes.join(' · ') || t('ahorro.empieza'));
}

/* ============ Zonas y mapa ============ */

async function cargarBase() {
  const [cfg, z] = await Promise.all([
    api('GET', '/api/v1/config'),
    api('GET', '/api/v1/zonas'),
  ]);
  estado.config = cfg;
  estado.zonas = z.zonas;

  const opciones = estado.zonas
    .map((zz, i) => `<option value="${i}">${escapar(zz.nombre)}</option>`).join('');
  for (const id of ['origen', 'destino', 'o-origen', 'o-destino', 'c-origen', 'c-destino']) {
    $(id).innerHTML = opciones;
  }
  $('o-vehiculo').innerHTML = cfg.vehiculos
    .map((v) => `<option value="${v.id}">${escapar(v.nombre)} — ${escapar(t('ofrecer.plazas', { n: v.plazas }))}</option>`).join('');

  // Por clave, no por nombre: renombrar un sitio no puede romper esto.
  const centro = estado.zonas.findIndex((zz) => zz.clave === 'downtown');
  const aero = estado.zonas.findIndex((zz) => zz.clave === 'airport');
  if (centro >= 0 && aero >= 0) {
    for (const id of ['origen', 'o-origen', 'c-origen']) $(id).value = centro;
    for (const id of ['destino', 'o-destino', 'c-destino']) $(id).value = aero;
  }
}

const MARGEN = 76;

function encuadre() {
  const lats = estado.zonas.map((z) => z.lat);
  const lngs = estado.zonas.map((z) => z.lng);
  const minLat = Math.min(...lats), maxLat = Math.max(...lats);
  const minLng = Math.min(...lngs), maxLng = Math.max(...lngs);
  const cosLat = Math.cos((minLat + maxLat) / 2 * Math.PI / 180);

  const anchoGeo = Math.max((maxLng - minLng) * cosLat, 1e-9);
  const altoGeo = Math.max(maxLat - minLat, 1e-9);
  const escala = 740 / Math.max(anchoGeo, altoGeo);

  const dibujo = anchoGeo * escala + 2 * MARGEN;
  // Con la portada delante, el texto tapa la mitad izquierda: se ensancha el
  // lienzo por la izquierda para que el mapa caiga en la mitad que se ve.
  const conPortada = !estado.usuario && window.innerWidth > 880;
  const hueco = conPortada ? dibujo * 1.05 : 0;

  return { maxLat, minLng, cosLat, escala, hueco,
    ancho: dibujo + hueco, alto: altoGeo * escala + 2 * MARGEN };
}

function proyectar(lat, lng, e) {
  return {
    x: e.hueco + MARGEN + (lng - e.minLng) * e.cosLat * e.escala,
    y: MARGEN + (e.maxLat - lat) * e.escala,
  };
}

function nombreCorto(n) { return n.split(/ [(\/]/)[0].trim(); }

function dibujarMapa() {
  const svg = $('mapa');
  if (!estado.zonas.length) return;

  const e = encuadre();
  svg.setAttribute('viewBox', `0 0 ${e.ancho.toFixed(0)} ${e.alto.toFixed(0)}`);
  const capas = [malla(e)];

  estado.resultados.forEach((m, i) => {
    const pts = m.trip.route.map((p) => proyectar(p.lat, p.lng, e));
    capas.push(`<polyline class="trazo-oferta${estado.seleccionado === i ? ' activo' : ''}"
      points="${pts.map((p) => `${p.x.toFixed(1)},${p.y.toFixed(1)}`).join(' ')}"/>`);
  });

  const pedido = trayectoPedido();
  if (pedido) {
    const a = proyectar(pedido.origen.lat, pedido.origen.lng, e);
    const b = proyectar(pedido.destino.lat, pedido.destino.lng, e);
    const largo = Math.hypot(b.x - a.x, b.y - a.y);
    if (estado.seleccionado !== null && estado.resultados[estado.seleccionado]) {
      capas.push(`<line class="trazo-compartido" x1="${a.x}" y1="${a.y}" x2="${b.x}" y2="${b.y}"/>`);
    }
    capas.push(`<line class="trazo-mio${ANIM ? ' dibuja' : ''}" style="--largo:${largo.toFixed(0)}"
      x1="${a.x.toFixed(1)}" y1="${a.y.toFixed(1)}" x2="${b.x.toFixed(1)}" y2="${b.y.toFixed(1)}"/>`);
  }

  estado.zonas.forEach((z, i) => {
    const p = proyectar(z.lat, z.lng, e);
    const elegida = pedido && (i === pedido.iOrigen || i === pedido.iDestino);
    // Las etiquetas de la mitad inferior van debajo: arriba se solapaban con
    // las del vecino de encima.
    const debajo = p.y > e.alto / 2;
    capas.push(`
      <g class="zona" data-zona="${i}" role="button" tabindex="0"
         aria-label="${escapar(z.nombre)}">
        <circle class="zona-punto${elegida ? ' elegida' : ''}" cx="${p.x.toFixed(1)}" cy="${p.y.toFixed(1)}" r="${elegida ? 7 : 5}"/>
        <circle class="zona-halo${elegida && ANIM ? ' viva' : ''}" cx="${p.x.toFixed(1)}" cy="${p.y.toFixed(1)}" r="7"/>
        <text class="zona-texto${elegida ? ' elegida' : ''}" x="${p.x.toFixed(1)}"
              y="${(p.y + (debajo ? 23 : -14)).toFixed(1)}">${escapar(nombreCorto(z.nombre))}</text>
      </g>`);
  });

  capas.push(escalaGrafica(e));
  svg.innerHTML = capas.join('');

  svg.querySelectorAll('.zona').forEach((g) => {
    const elegir = () => elegirZona(Number(g.dataset.zona));
    g.addEventListener('click', elegir);
    g.addEventListener('keydown', (ev) => {
      if (ev.key === 'Enter' || ev.key === ' ') { ev.preventDefault(); elegir(); }
    });
  });
}

/* malla une cada zona con las dos más cercanas. Dibuja el área de servicio y
   da cuerpo al mapa sin inventarse actividad: unas líneas pulsando entre
   ciudades parecerían viajes en curso, y todavía no los hay. */
function malla(e) {
  const puntos = estado.zonas.map((z) => proyectar(z.lat, z.lng, e));
  const hechas = new Set();
  const lineas = [];

  puntos.forEach((a, i) => {
    const cercanas = puntos
      .map((b, j) => ({ j, d: Math.hypot(b.x - a.x, b.y - a.y) }))
      .filter((x) => x.j !== i)
      .sort((x, y) => x.d - y.d)
      .slice(0, 2);

    for (const { j } of cercanas) {
      const clave = i < j ? `${i}-${j}` : `${j}-${i}`;
      if (hechas.has(clave)) continue;
      hechas.add(clave);
      const b = puntos[j];
      lineas.push(`<line x1="${a.x.toFixed(1)}" y1="${a.y.toFixed(1)}" x2="${b.x.toFixed(1)}" y2="${b.y.toFixed(1)}"/>`);
    }
  });
  return `<g class="malla" aria-hidden="true">${lineas.join('')}</g>`;
}

/* Sin escala, un mapa esquemático no deja juzgar si un desvío son cinco
   minutos o media hora. */
function escalaGrafica(e) {
  const kmPorGrado = 111.32;
  const objetivo = (e.ancho / e.escala) * kmPorGrado / 5;
  const km = [1, 2, 5, 10, 20, 50].reduce(
    (mejor, v) => (Math.abs(v - objetivo) < Math.abs(mejor - objetivo) ? v : mejor), 1);
  const largo = (km / kmPorGrado) * e.escala;
  const x = e.hueco + MARGEN, y = e.alto - 24;
  return `<g aria-hidden="true" opacity=".7">
    <line x1="${x}" y1="${y}" x2="${(x + largo).toFixed(1)}" y2="${y}" stroke="#5f6d7b" stroke-width="1.5"/>
    <line x1="${x}" y1="${y - 4}" x2="${x}" y2="${y + 4}" stroke="#5f6d7b" stroke-width="1.5"/>
    <line x1="${(x + largo).toFixed(1)}" y1="${y - 4}" x2="${(x + largo).toFixed(1)}" y2="${y + 4}" stroke="#5f6d7b" stroke-width="1.5"/>
    <text x="${(x + largo / 2).toFixed(1)}" y="${y - 9}" class="zona-texto">${km} km</text>
  </g>`;
}

/* Pulsar una zona rellena primero el origen y luego el destino: es el gesto que
   la gente espera y evita tener que explicar nada. */
let siguienteEsDestino = false;
function elegirZona(i) {
  const pre = estado.modo === 'ofrecer' ? 'o-' : '';
  $(pre + (siguienteEsDestino ? 'destino' : 'origen')).value = i;
  siguienteEsDestino = !siguienteEsDestino;
  actualizarSuelo();
  dibujarMapa();
}

function trayectoPedido() {
  if (!estado.zonas.length || !estado.usuario) return null;
  const pre = estado.modo === 'ofrecer' ? 'o-' : '';
  const iO = Number($(pre + 'origen').value), iD = Number($(pre + 'destino').value);
  if (Number.isNaN(iO) || Number.isNaN(iD) || iO === iD) return null;
  return { iOrigen: iO, iDestino: iD, origen: estado.zonas[iO], destino: estado.zonas[iD] };
}

/* ============ Calculadora de la portada ============ */

function calcular() {
  const a = estado.zonas[Number($('c-origen').value)];
  const b = estado.zonas[Number($('c-destino').value)];
  if (!a || !b || a === b) {
    $('c-solo').textContent = $('c-junto').textContent = '—';
    $('c-pie').textContent = t('portada.elige');
    return;
  }
  const km = distanciaKm(a, b);
  const solo = costeEstimado(km);
  // Compartiendo el camino entero con otra persona, el coste se parte a la
  // mitad: es exactamente lo que hace el reparto por tramos.
  const junto = Math.round(solo / 2);

  $('c-solo').textContent = euros(solo);
  $('c-junto').textContent = euros(junto);
  $('c-pie').innerHTML = t('portada.ahorro', {
    km: km.toFixed(1), ahorro: `<b>${escapar(euros(solo - junto))}</b>`,
  });
}

/* ============ Buscar ============ */

async function buscar() {
  const tr = trayectoPedido();
  if (!tr) { toast(t('aviso.lugares.t'), parrafo(t('aviso.lugares.d')), 'error'); return; }

  const desde = new Date($('cuando').value || Date.now());
  const margen = Number($('margen').value) * 60000;

  estado.buscando = true;
  $('btn-buscar').disabled = true;
  $('resultados-caja').classList.remove('oculto');
  $('resultados').innerHTML = '<div class="hueso hueso-viaje"></div><div class="hueso hueso-viaje"></div>';

  try {
    const r = await api('POST', '/api/v1/search', {
      pickup: { lat: tr.origen.lat, lng: tr.origen.lng },
      dropoff: { lat: tr.destino.lat, lng: tr.destino.lng },
      earliest_departure: new Date(desde.getTime() - margen).toISOString(),
      latest_departure: new Date(desde.getTime() + margen).toISOString(),
      seats: 1,
    });
    estado.resultados = r.matches || [];
    estado.seleccionado = estado.resultados.length ? 0 : null;
    pintarResultados();
    dibujarMapa();
  } catch (e) {
    explicarError(e);
    $('resultados').innerHTML = '';
  } finally {
    estado.buscando = false;
    $('btn-buscar').disabled = false;
  }
}

function pintarResultados() {
  const n = estado.resultados.length;
  $('resultados-titulo').textContent = n
    ? (n === 1 ? t('res.titulo.uno') : t('res.titulo.varios', { n }))
    : t('res.sin');

  if (!n) {
    $('resultados').innerHTML = `
      <div class="vacio">
        <p>${escapar(t('res.vacio.titulo'))}</p>
        <button class="btn-2 btn-fino" id="vacio-ofrecer">${escapar(t('res.vacio.boton'))}</button>
      </div>`;
    $('vacio-ofrecer')?.addEventListener('click', () => cambiarModo('ofrecer'));
    return;
  }

  $('resultados').innerHTML = estado.resultados.map((m, i) => {
    const v = m.trip;
    const soloYo = costeEstimado(m.shared_km);
    const ahorro = soloYo - m.estimated_price_cents;
    const cobertura = Math.min(100, Math.round(m.coverage * 100));

    return `
      <article class="viaje ${i === estado.seleccionado ? 'activo' : ''}" data-i="${i}"
               role="button" tabindex="0" style="animation-delay:${i * 40}ms">
        <div class="viaje-cab">
          <div class="ruta">${escapar(v.origin.name)} → ${escapar(v.destination.name)}
            <small>${escapar(fecha(new Date(v.departure_time)))}</small>
          </div>
          <div class="precio">
            <b>${euros(m.estimated_price_cents)}</b>
            ${ahorro > 0 ? `<s>${euros(soloYo)}</s>` : ''}
          </div>
        </div>
        <div class="solape"><i style="width:${cobertura}%"></i></div>
        <div class="datos">
          <span>${escapar(t('res.compartis', { km: m.shared_km.toFixed(1) }))}</span>
          <span>${escapar(t('res.apie', { m: (m.pickup_walk_km * 1000).toFixed(0) }))}</span>
          <span>${escapar(t('res.plazas', { n: v.seats_available }))}</span>
        </div>
        <div class="viaje-pie">
          <span class="nivel nivel-${claseNivel(v.nivel_exigido)}">${escapar(t('res.exige', { nivel: t('nivel.' + v.nivel_exigido) }))}</span>
          <button class="btn-1 btn-fino" data-reservar="${i}">${escapar(t('res.pedir'))}</button>
        </div>
      </article>`;
  }).join('');

  $('resultados').querySelectorAll('.viaje').forEach((el) => {
    const sel = () => { estado.seleccionado = Number(el.dataset.i); pintarResultados(); dibujarMapa(); };
    el.addEventListener('click', (ev) => { if (ev.target.dataset.reservar === undefined) sel(); });
    el.addEventListener('keydown', (ev) => {
      if (ev.key === 'Enter' || ev.key === ' ') { ev.preventDefault(); sel(); }
    });
  });
  $('resultados').querySelectorAll('[data-reservar]').forEach((b) =>
    b.addEventListener('click', () => reservar(Number(b.dataset.reservar), b)));
}

async function reservar(i, boton) {
  const m = estado.resultados[i];
  const tr = trayectoPedido();
  boton.disabled = true;
  boton.textContent = t('res.pidiendo');
  try {
    await api('POST', `/api/v1/trips/${m.trip.id}/bookings`, {
      pickup: { name: tr.origen.nombre, point: { lat: tr.origen.lat, lng: tr.origen.lng } },
      dropoff: { name: tr.destino.nombre, point: { lat: tr.destino.lat, lng: tr.destino.lng } },
      seats: 1,
    });
    await Promise.all([buscar(), refrescar()]);
    pintar();
    toast(t('aviso.pedida.t'), parrafo(t('aviso.pedida.d')), 'bien');
  } catch (e) {
    explicarError(e);
    boton.disabled = false;
    boton.textContent = t('res.pedir');
  }
}

/* ============ Publicar ============ */

function actualizarSuelo() {
  const v = estado.config?.vehiculos.find((x) => x.id === $('o-vehiculo').value);
  if (!v) return;
  $('o-suelo').innerHTML = `<strong>${escapar(v.nombre)}:</strong> ${escapar(textoSuelo(v.plazas))}`;
}

async function publicar() {
  const tr = trayectoPedido();
  if (!tr) { toast(t('aviso.lugares.t'), parrafo(t('aviso.lugares.d')), 'error'); return; }
  if (!$('o-cuando').value) { toast(t('aviso.hora.t'), '', 'error'); $('o-cuando').focus(); return; }

  // La tarifa es lo que verá y aceptará quien se suba, así que sin ella no se
  // publica: nadie debería comprometerse a un precio que aún no existe.
  const tarifa = parseFloat($('o-tarifa').value || '');
  if (!Number.isFinite(tarifa) || tarifa <= 0) {
    toast(t('aviso.tarifa.t'), parrafo(t('aviso.tarifa.d')), 'error');
    $('o-tarifa').focus();
    return;
  }

  const btn = $('btn-publicar');
  btn.disabled = true; btn.textContent = t('ofrecer.publicando');
  try {
    await api('POST', '/api/v1/trips', {
      origin: { name: tr.origen.nombre, point: { lat: tr.origen.lat, lng: tr.origen.lng } },
      destination: { name: tr.destino.nombre, point: { lat: tr.destino.lat, lng: tr.destino.lng } },
      departure_time: new Date($('o-cuando').value).toISOString(),
      vehicle: $('o-vehiculo').value,
      min_trust_level: $('o-nivel').value,
      tarifa_declarada_cents: Math.round(tarifa * 100),
    });
    await refrescar();
    pintar();
    toast(t('aviso.publicado.t'), parrafo(t('aviso.publicado.d')), 'bien');
  } catch (e) {
    explicarError(e);
  } finally {
    btn.disabled = false; btn.textContent = t('ofrecer.boton');
  }
}

/* ============ Mis trayectos ============ */

async function cargarMisTrayectos() {
  // Los propios en cualquier estado: filtrar los abiertos hacía desaparecer un
  // trayecto en cuanto se llenaba, justo cuando hay que cerrarlo.
  const r = await api('GET', '/api/v1/me/trips');
  estado.mios = r.trips || [];
  for (const t of estado.mios) {
    try {
      const b = await api('GET', `/api/v1/trips/${t.id}/bookings`);
      t.peticiones = (b.bookings || []).filter((x) => x.status === 'pending');
      t.confirmadas = (b.bookings || []).filter((x) => x.status === 'confirmed');
      // Quién pide la plaza, no solo cuántas. Aceptar a alguien te hace
      // responsable de su conducta en el vehículo: decidir eso sin saber quién
      // es ni qué dicen de él es decidir a ciegas.
      await Promise.all(t.peticiones.map(async (p) => {
        try { p.perfil = await api('GET', `/api/v1/users/${p.passenger_id}/confianza`); } catch { /* seguimos sin perfil */ }
      }));
    } catch { t.peticiones = []; t.confirmadas = []; }
  }
}

function pintarMisTrayectos() {
  if (!estado.mios.length) { $('mios-caja').classList.add('oculto'); return; }
  $('mios-caja').classList.remove('oculto');

  $('mios').innerHTML = estado.mios.map((v) => {
    const peticiones = (v.peticiones || []).map((p) => {
      const perfil = p.perfil;
      const quien = perfil ? `
        <div class="valorar-quien" style="display:flex;align-items:center;gap:8px;flex-wrap:wrap">
          <b>${escapar(perfil.nombre)}</b>
          <span class="nivel nivel-${claseNivel(perfil.nivel)}">${escapar(t('nivel.' + perfil.nivel))}</span>
          ${nota(perfil.estadisticas)}
        </div>` : '';
      return `
      <div style="margin-top:11px;padding-top:11px;border-top:1px solid var(--borde)">
        ${quien}
        <div class="fila">
          <span class="tenue">${escapar(t('mios.piden', { n: p.seats, precio: euros(p.price_cents) }))}</span>
          <span style="display:flex;gap:6px">
            <button class="btn-1 btn-fino" data-aceptar="${p.id}">${escapar(t('mios.aceptar'))}</button>
            <button class="btn-2 btn-fino" data-rechazar="${p.id}">${escapar(t('mios.no'))}</button>
          </span>
        </div>
      </div>`;
    }).join('');

    const conf = (v.confirmadas || []).length;
    return `
      <article class="viaje" style="cursor:default">
        <div class="viaje-cab">
          <div class="ruta">${escapar(v.origin.name)} → ${escapar(v.destination.name)}
            <small>${escapar(fecha(new Date(v.departure_time)))}</small>
          </div>
          <span class="nivel nivel-${claseNivel(v.nivel_exigido)}">${escapar(t('estado.' + v.status))}</span>
        </div>
        <div class="datos">
          <span><b>${v.distance_km}</b> km</span>
          <span>${escapar(t('mios.libres', { libres: v.seats_available, total: v.seats_total }))}</span>
          ${conf ? `<span>${escapar(t('mios.confirmadas', { n: conf }))}</span>` : ''}
          <span>${escapar(t(v.route_source === 'osrm' ? 'mios.rutareal' : 'mios.rutaest'))}</span>
        </div>
        ${peticiones}
        ${cierreDe(v)}
      </article>`;
  }).join('');

  $('mios').querySelectorAll('[data-abrir-cierre]').forEach((b) =>
    b.addEventListener('click', () => {
      cierreAbierto = b.dataset.abrirCierre;
      pintarMisTrayectos();
      $('importe-' + cierreAbierto)?.focus();
    }));
  $('mios').querySelectorAll('[data-cancelar-cierre]').forEach((b) =>
    b.addEventListener('click', () => { cierreAbierto = null; pintarMisTrayectos(); }));
  $('mios').querySelectorAll('[data-cerrar-viaje]').forEach((b) =>
    b.addEventListener('click', () => cerrarViaje(b.dataset.cerrarViaje, b)));
  $('mios').querySelectorAll('[data-repercutir]').forEach((b) =>
    b.addEventListener('click', () => repercutir(b.dataset.repercutir, b)));

  $('mios').querySelectorAll('[data-aceptar]').forEach((b) =>
    b.addEventListener('click', () => decidir(b.dataset.aceptar, true, b)));
  $('mios').querySelectorAll('[data-rechazar]').forEach((b) =>
    b.addEventListener('click', () => decidir(b.dataset.rechazar, false, b)));
}

/* ============ Incidencias ============ */

async function cargarIncidencias() {
  const r = await api('GET', '/api/v1/me/incidencias');
  estado.incidencias = r.incidencias || [];
}

function pintarIncidencias() {
  const vivas = (estado.incidencias || []).filter(
    (i) => i.estado !== 'retirada' && i.estado !== 'aceptada');
  const caja = $('incidencias-caja');
  caja.classList.toggle('oculto', !(estado.incidencias || []).length);
  if (!(estado.incidencias || []).length) return;

  $('incidencias').innerHTML = estado.incidencias.map((i) => {
    const mia = i.atribuida_a === estado.usuario.id;
    const puedeResponder = mia && i.estado === 'declarada';
    const puedeRetirar = !mia && i.estado === 'declarada';

    return `<article class="viaje" style="cursor:default">
      <div class="viaje-cab">
        <div class="ruta">${escapar(i.descripcion || t('inc.titulo'))}
          <small>${escapar(t('inc.estado.' + i.estado))}</small>
        </div>
        <div class="precio"><b>${euros(i.importe_cents)}</b></div>
      </div>
      ${puedeResponder ? `<div class="viaje-pie">
        <button class="btn-2 btn-fino" data-inc-no="${i.id}">${escapar(t('inc.discutir'))}</button>
        <button class="btn-1 btn-fino" data-inc-si="${i.id}">${escapar(t('inc.aceptar'))}</button>
      </div>` : ''}
      ${puedeRetirar ? `<div class="viaje-pie">
        <span class="tenue">${escapar(t('inc.nota'))}</span>
        <button class="btn-2 btn-fino" data-inc-retirar="${i.id}">${escapar(t('inc.retirar'))}</button>
      </div>` : ''}
    </article>`;
  }).join('');

  const responder = async (id, acepta, boton) => {
    boton.disabled = true;
    try {
      await api('POST', `/api/v1/incidencias/${id}/responder`, { acepta });
      await refrescar(); pintar();
      toast(t('aviso.inc.respondida.t'), '', 'bien');
    } catch (e) { explicarError(e); boton.disabled = false; }
  };
  $('incidencias').querySelectorAll('[data-inc-si]').forEach((b) =>
    b.addEventListener('click', () => responder(b.dataset.incSi, true, b)));
  $('incidencias').querySelectorAll('[data-inc-no]').forEach((b) =>
    b.addEventListener('click', () => responder(b.dataset.incNo, false, b)));
  $('incidencias').querySelectorAll('[data-inc-retirar]').forEach((b) =>
    b.addEventListener('click', async () => {
      b.disabled = true;
      try {
        await api('POST', `/api/v1/incidencias/${b.dataset.incRetirar}/retirar`, {});
        await refrescar(); pintar();
      } catch (e) { explicarError(e); b.disabled = false; }
    }));
  void vivas;
}

/* cierreAbierto es el trayecto cuyo formulario de cierre está desplegado. Solo
   uno a la vez: desplegarlos todos llenaría el panel de campos vacíos. */
let cierreAbierto = null;

/* cierreDe pinta el cierre de un viaje que ya se puede dar por hecho.
 *
 * Sin esto el bucle del producto no se cerraba nunca: ningún viaje se
 * completaba, así que no se anotaba nada en el libro y el ahorro acumulado se
 * quedaba siempre en cero. */
function cierreDe(v) {
  // Un viaje ya cerrado deja de poder cerrarse, pero sigue pudiendo generar
  // cargos: la flota cobra las tasas de limpieza después.
  if (v.status === 'completed') return repercutirDe(v);
  if (v.status === 'cancelled') return '';
  // Solo tiene sentido cerrar un viaje que ya ha salido y que alguien
  // compartió: sin pasajeros no hay nada que repartir.
  const haSalido = new Date(v.departure_time) <= new Date();
  if (!haSalido || !(v.confirmadas || []).length) return '';

  if (cierreAbierto !== v.id) {
    return `<div class="cerrar">
      <button class="btn-2 btn-fino" data-abrir-cierre="${v.id}">${escapar(t('cerrar.abrir'))}</button>
    </div>`;
  }

  return `<div class="cerrar">
    <h3>${escapar(t('cerrar.titulo'))}</h3>
    <p>${escapar(t('cerrar.explica'))}</p>
    <div class="campos-2">
      <div class="campo">
        <label for="importe-${v.id}">${escapar(t('cerrar.importe'))}</label>
        <input id="importe-${v.id}" type="number" step="0.01" min="0"
               placeholder="${escapar(t('cerrar.importe.ph'))}">
      </div>
      <div class="campo">
        <label for="ref-${v.id}">${escapar(t('cerrar.ref'))}</label>
        <input id="ref-${v.id}" placeholder="${escapar(t('cerrar.ref.ph'))}">
      </div>
    </div>
    <div class="acciones">
      <button class="btn-1 btn-fino" data-cerrar-viaje="${v.id}">${escapar(t('cerrar.boton'))}</button>
      <button class="btn-2 btn-fino" data-cancelar-cierre="1">${escapar(t('cerrar.cancelar'))}</button>
    </div>
  </div>`;
}

/* repercutirDe deja a quien organiza trasladar un cargo de la flota a quien lo
   causó. La flota se lo cobra a quien pidió el coche aunque el destrozo lo
   hiciera otro; sin esto asumiría esa responsabilidad sin herramienta alguna. */
function repercutirDe(v) {
  const pasajeros = (v.confirmadas || []);
  if (!pasajeros.length) return '';

  if (cierreAbierto !== v.id) {
    return `<div class="cerrar">
      <button class="btn-2 btn-fino" data-abrir-cierre="${v.id}">${escapar(t('inc.declarar'))}</button>
    </div>`;
  }
  return `<div class="cerrar">
    <h3>${escapar(t('inc.declarar'))}</h3>
    <p>${escapar(t('inc.nota'))}</p>
    <div class="campos-2">
      <div class="campo">
        <label for="inc-imp-${v.id}">${escapar(t('inc.importe'))}</label>
        <input id="inc-imp-${v.id}" type="number" step="0.01" min="0" placeholder="50.00">
      </div>
      <div class="campo">
        <label for="inc-quien-${v.id}">${escapar(t('inc.recibida', { quien: '', importe: '', destino: '' })).trim() || 'A quién'}</label>
        <select id="inc-quien-${v.id}">
          ${pasajeros.map((p) => `<option value="${p.passenger_id}">${escapar(p.pickup.name)}</option>`).join('')}
        </select>
      </div>
    </div>
    <div class="campo">
      <label for="inc-motivo-${v.id}">${escapar(t('inc.motivo'))}</label>
      <input id="inc-motivo-${v.id}" placeholder="${escapar(t('inc.motivo.ph'))}">
    </div>
    <div class="acciones">
      <button class="btn-1 btn-fino" data-repercutir="${v.id}">${escapar(t('inc.enviar'))}</button>
      <button class="btn-2 btn-fino" data-cancelar-cierre="1">${escapar(t('cerrar.cancelar'))}</button>
    </div>
  </div>`;
}

async function repercutir(tripID, boton) {
  const importe = parseFloat($('inc-imp-' + tripID)?.value || '');
  if (!Number.isFinite(importe) || importe <= 0) {
    toast(t('inc.importe'), '', 'error');
    return;
  }
  boton.disabled = true;
  try {
    await api('POST', `/api/v1/trips/${tripID}/incidencias`, {
      atribuida_a: $('inc-quien-' + tripID).value,
      tipo: 'limpieza',
      importe_cents: Math.round(importe * 100),
      descripcion: ($('inc-motivo-' + tripID)?.value || '').trim(),
    });
    cierreAbierto = null;
    await refrescar(); pintar();
    toast(t('aviso.inc.declarada.t'), parrafo(t('aviso.inc.declarada.d')), 'bien');
  } catch (e) {
    explicarError(e);
    boton.disabled = false;
  }
}

async function cerrarViaje(tripID, boton) {
  const importe = parseFloat($('importe-' + tripID)?.value || '');
  if (!Number.isFinite(importe) || importe <= 0) {
    toast(t('aviso.importe.t'), parrafo(t('aviso.importe.d')), 'error');
    $('importe-' + tripID)?.focus();
    return;
  }

  boton.disabled = true;
  boton.textContent = t('cerrar.cerrando');
  try {
    await api('POST', `/api/v1/trips/${tripID}/completar`, {
      // El importe se manda en céntimos, que es como lo lleva todo el resto
      // del sistema: los decimales en coma flotante no tocan el dinero.
      importe_real_cents: Math.round(importe * 100),
      ref_viaje: ($('ref-' + tripID)?.value || '').trim(),
    });
    cierreAbierto = null;
    await refrescar();
    pintar();
    toast(t('aviso.cerrado.t'), parrafo(t('aviso.cerrado.d')), 'bien');
  } catch (e) {
    explicarError(e);
    boton.disabled = false;
    boton.textContent = t('cerrar.boton');
  }
}

/* Aceptar a alguien obliga a asumir la responsabilidad que imponen los términos
   del robotaxi. No se puede aceptar sin verlo. */
async function decidir(id, aceptar, boton) {
  if (aceptar && !confirm(t('confirmar.responsabilidad'))) return;

  boton.disabled = true;
  try {
    await api('POST', `/api/v1/bookings/${id}/decision`,
      { accept: aceptar, acepta_responsabilidad: aceptar });
    await refrescar();
    pintar();
    toast(t(aceptar ? 'aviso.confirmada.t' : 'aviso.rechazada.t'),
      aceptar ? parrafo(t('aviso.confirmada.d')) : '', 'bien');
  } catch (e) {
    explicarError(e);
    boton.disabled = false;
  }
}

/* ============ Pintado ============ */

function claseNivel(n) {
  return ({ 'nuevo': 'nuevo', 'básico': 'basico', 'verificado': 'verificado', 'veterano': 'veterano' })[n] || 'nuevo';
}

function pintar() {
  const dentro = !!estado.usuario;
  document.querySelector('main').classList.toggle('sin-sesion', !dentro);

  $('confianza').classList.toggle('oculto', !dentro);
  $('modo').classList.toggle('oculto', !dentro);
  $('buscar').classList.toggle('oculto', !dentro || estado.modo !== 'buscar');
  $('ofrecer').classList.toggle('oculto', !dentro || estado.modo !== 'ofrecer');

  const nivel = estado.perfil?.nivel || 'nuevo';
  $('sesion').innerHTML = dentro
    ? `<span class="nivel nivel-${claseNivel(nivel)}">${escapar(t('nivel.' + nivel))}</span>
       <span class="tenue">${escapar(estado.usuario.name)}</span>
       <button class="enlace" id="btn-salir">${escapar(t('cab.salir'))}</button>`
    : `<button class="btn-2 btn-fino" id="btn-entrar-cab">${escapar(t('cab.entrar'))}</button>`;

  pintarTerminos();

  if (dentro) {
    $('btn-salir').addEventListener('click', salir);
    pintarConfianza();
    pintarAhorro();
    pintarIncidencias();
    pintarValoraciones();
    pintarSuspension();
    pintarContactos();
    pintarViajeActivo();
    pintarSaldo();
    pintarMisTrayectos();
  } else {
    $('btn-entrar-cab').addEventListener('click', () => mostrarAcceso(false));
  }
}

function cambiarModo(modo) {
  estado.modo = modo;
  $('tab-buscar').setAttribute('aria-selected', String(modo === 'buscar'));
  $('tab-ofrecer').setAttribute('aria-selected', String(modo === 'ofrecer'));
  siguienteEsDestino = false;
  pintar();
  dibujarMapa();
}

/* ============ Recuperar contraseña ============ */

/* La vista tiene dos caras: pedir el enlace, y usarlo. Cuál se enseña lo decide
   si la dirección trae testigo, no un botón: quien llega desde el correo ya ha
   hecho su parte y solo tiene que elegir contraseña. */
function testigoDeLaURL() {
  const h = location.hash || '';
  return h.startsWith('#recuperar=') ? decodeURIComponent(h.slice('#recuperar='.length)) : '';
}

function mostrarRecuperar(testigo = '') {
  $('portada').classList.add('oculto');
  $('acceso').classList.add('oculto');
  $('recuperar').classList.remove('oculto');

  const conTestigo = !!testigo;
  $('rec-titulo').textContent = t(conTestigo ? 'rec.titulo.nueva' : 'rec.titulo');
  $('rec-sub').textContent = t(conTestigo ? 'rec.sub.nueva' : 'rec.sub');
  $('form-rec-pedir').classList.toggle('oculto', conTestigo);
  $('form-rec-cambiar').classList.toggle('oculto', !conTestigo);
  // El correo escrito en el acceso se arrastra: no hay que teclearlo otra vez.
  if (!conTestigo && $('email').value) $('rec-email').value = $('email').value;
  (conTestigo ? $('rec-clave') : $('rec-email')).focus();
}

async function pedirRecuperacion(ev) {
  ev.preventDefault();
  const btn = $('btn-rec-pedir');
  const antes = btn.textContent;
  btn.disabled = true;
  btn.textContent = t('rec.enviando');
  try {
    await api('POST', '/api/v1/auth/recuperar', { email: $('rec-email').value.trim() });
    // El mensaje es el mismo exista o no la cuenta: decir "ese correo no está
    // registrado" convertiría este formulario en un buscador de quién tiene
    // cuenta aquí.
    toast(t('rec.enviado.t'), parrafo(t('rec.enviado.d')), 'bien');
    volverAPortada();
  } catch (e) {
    if (!avisarSiCortaron(e)) toast(t('aviso.fallo.t'), parrafo(mensajeDeError(e)), 'error');
  } finally {
    btn.disabled = false;
    btn.textContent = antes;
  }
}

async function cambiarContrasena(ev) {
  ev.preventDefault();
  const btn = $('btn-rec-cambiar');
  const antes = btn.textContent;
  btn.disabled = true;
  btn.textContent = t('rec.guardando');
  try {
    await api('POST', '/api/v1/auth/recuperar/confirmar',
      { testigo: testigoDeLaURL(), password: $('rec-clave').value });
    toast(t('rec.hecho.t'), parrafo(t('rec.hecho.d')), 'bien');
    history.replaceState(null, '', location.pathname);
    $('rec-clave').value = '';
    mostrarAcceso(false);
  } catch (e) {
    if (!avisarSiCortaron(e)) toast(t('aviso.fallo.t'), parrafo(mensajeDeError(e)), 'error');
  } finally {
    btn.disabled = false;
    btn.textContent = antes;
  }
}

/* ============ Condiciones ============ */

/* Enseña el aviso cuando la redacción vigente no es la que esa persona aceptó.
   Sin esto, versionar las condiciones no serviría de nada: nadie se enteraría
   de que han cambiado. */
function pintarTerminos() {
  const caja = $('caja-terminos');
  const vigente = estado.config?.terminos_version;
  const alDia = !vigente || !estado.usuario || estado.usuario.terminos_version === vigente;
  caja.classList.toggle('oculto', alDia);
  if (!alDia) $('terminos-nuevas-d').innerHTML = textoDeCondicionesNuevas(vigente);
}

function textoDeCondicionesNuevas(version) {
  return escapar(t('legal.nuevas.d', { version }))
    .replace('{terminos}',
      `<a href="/terminos.html" target="_blank" rel="noopener">${escapar(t('legal.terminos'))}</a>`);
}

async function aceptarTerminos() {
  const btn = $('btn-aceptar-terminos');
  btn.disabled = true;
  try {
    estado.usuario = await api('POST', '/api/v1/me/terminos');
    toast(t('legal.aceptadas'), parrafo(t('legal.aceptadas.d')), 'bien');
    pintarTerminos();
  } catch (e) {
    toast(t('aviso.fallo.t'), parrafo(e.message), 'error');
  } finally {
    btn.disabled = false;
  }
}

/* ============ Valoraciones ============ */

/* La estrella, dibujada aquí para no depender de ninguna fuente de iconos: la
   política de seguridad solo deja cargar cosas de este servidor. */
const ESTRELLA = `<svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M12 2.6l2.9 5.9 6.5.9-4.7 4.6 1.1 6.4-5.8-3-5.8 3 1.1-6.4L2.6 9.4l6.5-.9L12 2.6z"/></svg>`;

/* nota compone la media de alguien. Sin valoraciones no enseña un 0: enseñar
   cero estrellas a quien acaba de llegar es acusarle de algo que no ha hecho. */
function nota(stats) {
  if (!stats || !stats.rating_count) {
    return `<span class="tenue" style="font-size:12px">${escapar(t('val.ninguna'))}</span>`;
  }
  const media = stats.rating.toLocaleString(idioma(), {
    minimumFractionDigits: 1, maximumFractionDigits: 1,
  });
  return `<span class="nota">${ESTRELLA}${escapar(t('val.media', { media, n: stats.rating_count }))}</span>`;
}

function pintarValoraciones() {
  const pendientes = estado.pendientes || [];
  $('valorar-caja').classList.toggle('oculto', !pendientes.length);
  if (!pendientes.length) return;

  $('valorar-lista').innerHTML = pendientes.map((p) => `
    <div class="valorar" data-val="${escapar(p.booking_id)}">
      <div class="valorar-quien">${escapar(t('val.con', { quien: p.nombre }))}</div>
      <div class="valorar-viaje">${escapar(t('val.viaje', {
        destino: p.destino, fecha: fecha(new Date(p.salida)),
      }))}</div>
      <div class="estrellas" role="group" aria-label="${escapar(t('val.enviar'))}">
        ${[1, 2, 3, 4, 5].map((n) => `<button type="button" data-estrella="${n}"
          aria-pressed="false" aria-label="${escapar(t('val.estrella', { n }))}">${ESTRELLA}</button>`).join('')}
      </div>
      <textarea data-comentario maxlength="500" data-t-ph="val.comentario.ph"
        placeholder="${escapar(t('val.comentario.ph'))}"></textarea>
      <div class="modal-pie">
        <button class="denunciar" data-denunciar="${escapar(p.sobre_id)}"
          data-nombre="${escapar(p.nombre)}" data-viaje="${escapar(p.trip_id)}">${escapar(t('den.abrir'))}</button>
        <button class="btn-1 btn-fino" data-enviar>${escapar(t('val.enviar'))}</button>
      </div>
    </div>`).join('');

  $('valorar-lista').querySelectorAll('.valorar').forEach(conectarValoracion);
  $('valorar-lista').querySelectorAll('[data-denunciar]').forEach((b) =>
    b.addEventListener('click', () => abrirDenuncia(b.dataset.denunciar, b.dataset.nombre, b.dataset.viaje)));
}

function conectarValoracion(caja) {
  let elegidas = 0;
  const botones = [...caja.querySelectorAll('[data-estrella]')];
  const pintarEstrellas = () => botones.forEach((b) =>
    b.setAttribute('aria-pressed', String(Number(b.dataset.estrella) <= elegidas)));

  botones.forEach((b) => b.addEventListener('click', () => {
    elegidas = Number(b.dataset.estrella);
    pintarEstrellas();
  }));

  caja.querySelector('[data-enviar]').addEventListener('click', async (ev) => {
    const boton = ev.currentTarget;
    if (!elegidas) {
      toast(t('val.elige'), parrafo(t('val.elige.d')), 'error');
      botones[4].focus();
      return;
    }
    const antes = boton.textContent;
    boton.disabled = true;
    boton.textContent = t('val.enviando');
    try {
      await api('POST', `/api/v1/bookings/${caja.dataset.val}/valoracion`, {
        estrellas: elegidas,
        comentario: caja.querySelector('[data-comentario]').value.trim(),
      });
      toast(t('val.hecho.t'), parrafo(t('val.hecho.d')), 'bien');
      await refrescar();
      pintar();
    } catch (e) {
      explicarError(e);
      boton.disabled = false;
      boton.textContent = antes;
    }
  });
}

/* pintarSuspension explica por qué de pronto no se puede publicar ni reservar.
   Sin este cartel, quien está suspendido solo ve que la app falla. */
function pintarSuspension() {
  const hasta = estado.usuario?.suspendido_hasta;
  const activa = hasta && new Date(hasta) > new Date();
  $('caja-suspension').classList.toggle('oculto', !activa);
  if (activa) {
    $('suspension-texto').textContent = t('susp.texto', {
      hasta: new Date(hasta).toLocaleDateString(idioma(), { day: 'numeric', month: 'long', year: 'numeric' }),
    });
  }
}

/* ============ Denuncias ============ */

let denunciaAbierta = null;

function abrirDenuncia(userID, nombre, tripID) {
  denunciaAbierta = { userID, tripID };
  $('den-sub').textContent = t('den.sub', { quien: nombre });
  $('den-texto').value = '';
  $('den-motivo').selectedIndex = 0;
  $('modal-denuncia').hidden = false;
  $('den-motivo').focus();
}

function cerrarDenuncia() {
  denunciaAbierta = null;
  $('modal-denuncia').hidden = true;
}

async function enviarDenuncia(ev) {
  ev.preventDefault();
  if (!denunciaAbierta) return;
  const texto = $('den-texto').value.trim();
  if (texto.length < 10) {
    toast(t('den.corto'), parrafo(t('den.corto.d')), 'error');
    $('den-texto').focus();
    return;
  }

  const boton = $('den-enviar');
  const antes = boton.textContent;
  boton.disabled = true;
  boton.textContent = t('den.enviando');
  try {
    await api('POST', `/api/v1/users/${denunciaAbierta.userID}/denunciar`, {
      motivo: $('den-motivo').value,
      descripcion: texto,
      trip_id: denunciaAbierta.tripID || '',
    });
    cerrarDenuncia();
    toast(t('den.hecho.t'), parrafo(t('den.hecho.d')), 'bien');
    await refrescar();
    pintar();
  } catch (e) {
    explicarError(e);
  } finally {
    boton.disabled = false;
    boton.textContent = antes;
  }
}

/* ============ Seguridad ============ */

/* Contactos de confianza. Sin ellos el botón de emergencia no tiene a quién
   avisar, así que la tarjeta se enseña aunque la lista esté vacía. */
function pintarContactos() {
  const cs = estado.contactos || [];
  $('contactos-caja').classList.remove('oculto');
  $('contactos').innerHTML = cs.map((c) => `
    <li>
      <span><b>${escapar(c.nombre)}</b> <span class="correo">${escapar(c.email)}</span></span>
      ${c.avisar_al_salir ? `<span class="avisa">${escapar(t('ctc.avisa'))}</span>` : ''}
      <button class="quitar" data-quitar="${escapar(c.id)}"
              aria-label="${escapar(t('ctc.quitar'))}">×</button>
    </li>`).join('');

  // Al llegar al tope el formulario desaparece: enseñar un formulario que va a
  // fallar es hacer perder el tiempo.
  const lleno = cs.length >= (estado.maxContactos || 3);
  $('form-contacto').classList.toggle('oculto', lleno);

  $('contactos').querySelectorAll('[data-quitar]').forEach((b) =>
    b.addEventListener('click', async () => {
      b.disabled = true;
      try {
        await api('DELETE', `/api/v1/me/contactos/${b.dataset.quitar}`);
        await cargarContactos();
        pintarContactos();
      } catch (e) { explicarError(e); b.disabled = false; }
    }));
}

async function cargarContactos() {
  try {
    const r = await api('GET', '/api/v1/me/contactos');
    estado.contactos = r.contactos || [];
    estado.maxContactos = r.maximo || 3;
  } catch {
    estado.contactos = [];
  }
}

async function anadirContacto(ev) {
  ev.preventDefault();
  const nombre = $('ctc-nombre').value.trim();
  const email = $('ctc-email').value.trim();
  if (!nombre || !email) {
    toast(t('ctc.faltan.t'), parrafo(t('ctc.faltan.d')), 'error');
    (nombre ? $('ctc-email') : $('ctc-nombre')).focus();
    return;
  }
  const boton = $('ctc-anadir');
  boton.disabled = true;
  try {
    await api('POST', '/api/v1/me/contactos', {
      nombre, email, avisar_al_salir: $('ctc-avisar').checked,
    });
    $('ctc-nombre').value = ''; $('ctc-email').value = '';
    await cargarContactos();
    pintarContactos();
    toast(t('ctc.hecho.t'), parrafo(t('ctc.hecho.d')), 'bien');
  } catch (e) {
    explicarError(e);
  } finally {
    boton.disabled = false;
  }
}

/* ============ Viaje en curso ============ */

async function cargarViajesActivos() {
  try {
    const r = await api('GET', '/api/v1/me/viajes-activos');
    estado.activos = r.viajes || [];
  } catch {
    estado.activos = [];
  }
}

function pintarViajeActivo() {
  const vs = estado.activos || [];
  $('viaje-activo').classList.toggle('oculto', !vs.length);
  if (!vs.length) return;

  $('activos').innerHTML = vs.map((v, i) => `
    <div class="activo" data-viaje="${escapar(v.trip_id)}">
      <div class="activo-ruta">${escapar(v.origen)} → ${escapar(v.destino)}</div>
      <div class="activo-cuando">${escapar(fecha(new Date(v.salida)))} ·
        ${escapar(t('papel.' + v.papel))}</div>
      <div class="activo-acciones">
        <button class="btn-2 btn-fino" data-compartir="${i}">${escapar(
          t(v.compartido ? 'seg.otro' : 'seg.compartir'))}</button>
        ${v.compartido ? `<button class="enlace" data-revocar="${escapar(v.trip_id)}"
          style="font-size:12px">${escapar(t('seg.revocar'))}</button>` : ''}
      </div>
      <div class="enlace-seg oculto" data-caja-enlace="${i}">
        <input readonly data-url="${i}" aria-label="${escapar(t('seg.enlace'))}">
        <button class="btn-2 btn-fino" data-copiar="${i}">${escapar(t('seg.copiar'))}</button>
      </div>
      ${v.alerta
        ? `<button class="alerta-viva" data-ver-alerta="${i}">⚠ ${escapar(t('alr.viva'))}</button>`
        : botonSOS(i)}
    </div>`).join('');

  vs.forEach((v, i) => conectarViajeActivo(v, i));
}

function botonSOS(i) {
  return `<button class="sos" data-sos="${i}"><i></i>
    <span>${escapar(t('sos.boton'))}</span>
    <small>${escapar(t('sos.pie'))}</small></button>`;
}

function conectarViajeActivo(v, i) {
  const caja = $('activos').querySelector(`[data-viaje="${CSS.escape(v.trip_id)}"]`);
  if (!caja) return;

  caja.querySelector(`[data-compartir="${i}"]`).addEventListener('click', async (ev) => {
    const boton = ev.currentTarget;
    boton.disabled = true;
    try {
      const enlace = await api('POST', `/api/v1/trips/${v.trip_id}/compartir`);
      const cajaEnlace = caja.querySelector(`[data-caja-enlace="${i}"]`);
      cajaEnlace.classList.remove('oculto');
      cajaEnlace.querySelector('input').value = enlace.url;
      await cargarViajesActivos();
    } catch (e) { explicarError(e); } finally { boton.disabled = false; }
  });

  const copiar = caja.querySelector(`[data-copiar="${i}"]`);
  if (copiar) {
    copiar.addEventListener('click', async () => {
      const campo = caja.querySelector(`[data-url="${i}"]`);
      campo.select();
      try {
        await navigator.clipboard.writeText(campo.value);
        toast(t('seg.copiado'), '', 'bien');
      } catch {
        // Sin permiso de portapapeles queda seleccionado para copiarlo a mano.
        toast(t('seg.copia.manual'), '', '');
      }
    });
  }

  const revocar = caja.querySelector('[data-revocar]');
  if (revocar) {
    revocar.addEventListener('click', async () => {
      revocar.disabled = true;
      try {
        await api('POST', `/api/v1/trips/${revocar.dataset.revocar}/dejar-de-compartir`);
        await cargarViajesActivos();
        pintarViajeActivo();
        toast(t('seg.revocado'), '', 'bien');
      } catch (e) { explicarError(e); revocar.disabled = false; }
    });
  }

  const verAlerta = caja.querySelector(`[data-ver-alerta="${i}"]`);
  if (verAlerta) verAlerta.addEventListener('click', () => abrirAlerta(v.alerta, v));

  const sos = caja.querySelector(`[data-sos="${i}"]`);
  if (sos) conectarSOS(sos, v);
}

/* ============ El botón de emergencia ============ */

/* Se mantiene pulsado. Un solo gesto —el que se puede hacer con la mano
   temblando— y que no se dispara solo en el bolsillo. Un diálogo de "¿estás
   seguro?" sería un paso más justo cuando no hay tiempo para pasos. */
const RETENCION_SOS = 1500;

function conectarSOS(boton, viaje) {
  let temporizador = null;

  const soltar = () => {
    clearTimeout(temporizador);
    temporizador = null;
    boton.classList.remove('armado');
  };
  const apretar = (ev) => {
    ev.preventDefault();
    if (temporizador) return;
    boton.classList.add('armado');
    temporizador = setTimeout(() => { soltar(); dispararSOS(boton, viaje); }, RETENCION_SOS);
  };

  boton.addEventListener('pointerdown', apretar);
  boton.addEventListener('pointerup', soltar);
  boton.addEventListener('pointerleave', soltar);
  boton.addEventListener('pointercancel', soltar);
  // Con teclado no hay "mantener": la barra espaciadora repite pero Enter no.
  // Quien navega con teclado dispara con Enter y confirma en el diálogo.
  boton.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); dispararSOS(boton, viaje); }
  });
}

/* posicionActual pide la ubicación sin bloquear. Si no llega en cinco segundos
   —o no hay permiso— la alerta sale igual: lo que no puede pasar es que el
   botón falle justo cuando hace falta. */
function posicionActual() {
  return new Promise((resolve) => {
    if (!navigator.geolocation) { resolve(null); return; }
    let contestado = false;
    const acabar = (v) => { if (!contestado) { contestado = true; resolve(v); } };
    setTimeout(() => acabar(null), 5000);
    navigator.geolocation.getCurrentPosition(
      (pos) => acabar({ lat: pos.coords.latitude, lng: pos.coords.longitude }),
      () => acabar(null),
      { enableHighAccuracy: true, timeout: 5000, maximumAge: 30000 });
  });
}

async function dispararSOS(boton, viaje) {
  boton.disabled = true;
  try {
    const punto = await posicionActual();
    const r = await api('POST', `/api/v1/trips/${viaje.trip_id}/emergencia`, {
      lat: punto ? punto.lat : null,
      lng: punto ? punto.lng : null,
      nota: '',
    });
    abrirAlerta(r.alerta, viaje, r.telefono_emergencias, !!punto);
    await cargarViajesActivos();
    pintarViajeActivo();
  } catch (e) {
    explicarError(e);
    boton.disabled = false;
  }
}

let alertaAbierta = null;

function abrirAlerta(alerta, viaje, telefono, conPosicion) {
  alertaAbierta = alerta;
  const tel = telefono || '911';
  $('alr-telefono').textContent = tel;
  $('alr-911').href = 'tel:' + tel;
  $('alr-sub').textContent = t('alr.sub', { destino: viaje.destino });

  const cuantos = (estado.contactos || []).length;
  $('alr-hecho').innerHTML = [
    cuantos ? t('alr.hecho.contactos', { n: cuantos }) : t('alr.hecho.sincontactos'),
    t('alr.hecho.operaciones'),
    conPosicion === false ? t('alr.hecho.sinposicion') : t('alr.hecho.posicion'),
  ].map((x) => `<li>${escapar(x)}</li>`).join('');

  $('modal-alerta').hidden = false;
  $('alr-911').focus();
}

function cerrarAlerta() {
  alertaAbierta = null;
  $('modal-alerta').hidden = true;
}

async function falsaAlarma() {
  if (!alertaAbierta) return;
  const boton = $('alr-falsa');
  boton.disabled = true;
  try {
    await api('POST', `/api/v1/alertas/${alertaAbierta.id}/retirar`);
    cerrarAlerta();
    toast(t('alr.retirada.t'), parrafo(t('alr.retirada.d')), 'bien');
    await cargarViajesActivos();
    pintarViajeActivo();
  } catch (e) {
    explicarError(e);
  } finally {
    boton.disabled = false;
  }
}

/* ============ Saldo del periodo ============ */

async function cargarSaldo() {
  try {
    estado.saldo = await api('GET', '/api/v1/me/saldo');
  } catch {
    estado.saldo = null;
  }
}

/* La tarjeta solo aparece cuando hay algo que contar. A quien no ha compartido
   ningún viaje, un "0,00 $" no le dice nada: le ocupa sitio. */
function pintarSaldo() {
  const s = estado.saldo;
  const hayAlgo = s && (s.apuntes > 0 || (s.movimientos || []).length > 0);
  $('caja-saldo').classList.toggle('oculto', !hayAlgo);
  if (!hayAlgo) return;

  const neto = s.pendiente_cents;
  const cifra = $('saldo-cifra');
  cifra.className = 'saldo-cifra ' + (neto > 0 ? 'debe' : neto < 0 ? 'cobra' : 'cero');
  cifra.textContent = neto > 0 ? t('saldo.debes', { importe: euros(neto) })
    : neto < 0 ? t('saldo.tedebemos', { importe: euros(-neto) })
      : t('saldo.cero');

  $('saldo-cuando').textContent = s.apuntes
    ? t('saldo.cierra', { fecha: soloFecha(new Date(s.proximo_cierre)) })
    : t('saldo.nadapendiente');

  $('saldo-desglose').innerHTML = desgloseDelSaldo(s, neto);

  const movs = s.movimientos || [];
  $('saldo-movs-caja').classList.toggle('oculto', !movs.length);
  $('saldo-movs').innerHTML = movs.map((m) => `
    <li>
      <span>${escapar(t('saldo.mov.' + m.tipo))}</span>
      <span class="estado">${escapar(t('saldo.estado.' + m.estado, {}, m.estado))}</span>
      <b>${escapar(euros(m.amount_cents))}</b>
    </li>`).join('');
}

/* desgloseDelSaldo arma las líneas que de verdad dicen algo.
   Las que valen cero se caen: una fila "0,00 $" no explica nada, y con el signo
   delante queda además un "−0,00 $" que no significa nada en ningún idioma. */
function desgloseDelSaldo(s, neto) {
  if (!s.apuntes) return '';
  const filas = [
    [t('saldo.debe'), s.debe_cents - s.comision_cents, ''],
    [t('saldo.comision'), s.comision_cents, ''],
    [t('saldo.leden'), s.le_deben_cents, '−'],
  ].filter(([, cents]) => cents !== 0);

  return filas.map(([etiqueta, cents, signo]) =>
    `<dt>${escapar(etiqueta)}</dt><dd>${signo}${escapar(euros(cents))}</dd>`).join('') +
    `<dt class="total">${escapar(t('saldo.neto'))}</dt>` +
    `<dd class="total">${escapar(euros(Math.abs(neto)))}</dd>`;
}

function soloFecha(d) {
  return d.toLocaleDateString(idioma(), { day: 'numeric', month: 'long' });
}

/* ============ Arranque ============ */

function horaPorDefecto() {
  const d = new Date(Date.now() + 2 * 3600 * 1000);
  d.setMinutes(0, 0, 0);
  return new Date(d.getTime() - d.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
}

/* repintar vuelve a componer todo lo que ya está en pantalla. Cambiar de
   idioma no debe obligar a recargar ni perder lo que había. */
function repintar() {
  aplicarTextos();
  document.querySelectorAll('[data-idioma]').forEach((b) =>
    b.setAttribute('aria-current', String(b.dataset.idioma === idioma())));
  actualizarSuelo();
  calcular();
  if (!$('acceso').classList.contains('oculto')) mostrarAcceso(esAlta);
  if (!$('recuperar').classList.contains('oculto')) mostrarRecuperar(testigoDeLaURL());
  pintar();
  if (estado.resultados.length) pintarResultados();
}

function conectar() {
  document.querySelectorAll('[data-idioma]').forEach((b) =>
    b.addEventListener('click', () => { cambiarIdioma(b.dataset.idioma); repintar(); }));

  $('cta-alta').addEventListener('click', () => mostrarAcceso(true));
  $('cta-entrar').addEventListener('click', () => mostrarAcceso(false));
  $('volver').addEventListener('click', volverAPortada);
  $('cambiar-modo').addEventListener('click', () => mostrarAcceso(!esAlta));
  $('form-acceso').addEventListener('submit', enviarAcceso);
  $('olvide').addEventListener('click', () => mostrarRecuperar());
  $('rec-volver').addEventListener('click', volverAPortada);
  $('form-rec-pedir').addEventListener('submit', pedirRecuperacion);
  $('form-rec-cambiar').addEventListener('submit', cambiarContrasena);
  $('btn-aceptar-terminos').addEventListener('click', aceptarTerminos);
  $('form-denuncia').addEventListener('submit', enviarDenuncia);
  $('form-contacto').addEventListener('submit', anadirContacto);
  $('alr-cerrar').addEventListener('click', cerrarAlerta);
  $('alr-falsa').addEventListener('click', falsaAlarma);
  $('den-cancelar').addEventListener('click', cerrarDenuncia);
  $('modal-denuncia').addEventListener('click', (e) => {
    if (e.target === $('modal-denuncia')) cerrarDenuncia();
  });

  // El enlace del correo puede abrirse con la app ya cargada: cambiar solo el
  // ancla no recarga nada, así que si no se atiende aquí, quien vuelve a la
  // pestaña que ya tenía abierta no ve el formulario y cree que el enlace está
  // roto.
  window.addEventListener('hashchange', () => {
    const testigo = testigoDeLaURL();
    if (testigo) mostrarRecuperar(testigo);
  });

  $('tab-buscar').addEventListener('click', () => cambiarModo('buscar'));
  $('tab-ofrecer').addEventListener('click', () => cambiarModo('ofrecer'));
  $('btn-buscar').addEventListener('click', buscar);
  $('btn-publicar').addEventListener('click', publicar);
  $('o-vehiculo').addEventListener('change', actualizarSuelo);

  for (const id of ['origen', 'destino', 'o-origen', 'o-destino']) {
    $(id).addEventListener('change', dibujarMapa);
  }
  for (const id of ['c-origen', 'c-destino']) {
    $(id).addEventListener('change', calcular);
  }

  const irAPendientes = () => $('mios-caja').scrollIntoView({ behavior: ANIM ? 'smooth' : 'auto', block: 'start' });
  $('pendientes').addEventListener('click', irAPendientes);
  $('pendientes').addEventListener('keydown', (e) => {
    if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); irAPendientes(); }
  });

  let temporizador;
  window.addEventListener('resize', () => {
    clearTimeout(temporizador);
    temporizador = setTimeout(dibujarMapa, 150);
  });

  // Escape cierra el acceso o la recuperación y vuelve a la portada.
  document.addEventListener('keydown', (e) => {
    if (e.key !== 'Escape') return;
    // El diálogo de la alerta no se cierra con Escape a propósito: perderlo
    // de un tecleo en mitad de una emergencia es lo último que hace falta.
    if (!$('modal-alerta').hidden) return;
    if (!$('modal-denuncia').hidden) { cerrarDenuncia(); return; }
    const abierto = !$('acceso').classList.contains('oculto') ||
      !$('recuperar').classList.contains('oculto');
    if (abierto) volverAPortada();
  });
}

async function arrancar() {
  aplicarTextos();
  conectar();
  $('cuando').value = horaPorDefecto();
  $('o-cuando').value = horaPorDefecto();

  try {
    await cargarBase();
  } catch {
    toast(t('aviso.nocarga.t'), parrafo(t('aviso.nocarga.d')), 'error');
    return;
  }
  repintar();

  // Un enlace de recuperación manda sobre todo lo demás: quien llega desde el
  // correo viene a una sola cosa.
  const testigo = testigoDeLaURL();
  if (testigo) {
    mostrarRecuperar(testigo);
    dibujarMapa();
    return;
  }

  if (estado.token) {
    try {
      estado.usuario = await api('GET', '/api/v1/me');
      await entrarEnLaApp();
      return;
    } catch {
      // Token caducado: se empieza de cero sin molestar.
      estado.token = '';
      localStorage.removeItem('token');
    }
  }
  pintar();
  dibujarMapa();
}

arrancar();
