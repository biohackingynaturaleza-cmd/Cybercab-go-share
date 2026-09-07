'use strict';

/* Cybercab Go Share — interfaz.
 *
 * Sin dependencias externas, a propósito: la política de seguridad de la página
 * solo permite scripts de este mismo servidor, así que no hay CDN que pueda
 * caerse, cambiar bajo los pies o rastrear a quien entra.
 *
 * El mapa es esquemático y no de calles porque lo que este producto tiene que
 * enseñar es si dos trayectos se solapan y cuánto. De navegar ya se encarga el
 * propio robotaxi. */

const $ = (id) => document.getElementById(id);

const estado = {
  token: localStorage.getItem('token') || '',
  usuario: null,
  perfil: null,
  zonas: [],
  modo: 'buscar',
  resultados: [],
  seleccionado: null,
  mios: [],
};

/* ---------- Cliente de la API ---------- */

async function api(metodo, ruta, cuerpo) {
  const cab = { 'Content-Type': 'application/json' };
  if (estado.token) cab['Authorization'] = 'Bearer ' + estado.token;

  const resp = await fetch(ruta, {
    method: metodo,
    headers: cab,
    body: cuerpo === undefined ? undefined : JSON.stringify(cuerpo),
  });

  let datos = null;
  try { datos = await resp.json(); } catch { /* respuesta sin cuerpo */ }

  if (!resp.ok) {
    const err = new Error((datos && datos.error) || `Error ${resp.status}`);
    err.status = resp.status;
    err.datos = datos || {};
    throw err;
  }
  return datos;
}

/* ---------- Avisos ---------- */

function avisar(texto, tipo = 'aviso', extra = '') {
  $('avisos').innerHTML =
    `<div class="aviso ${tipo}"><strong>${escapar(texto)}</strong>${extra}</div>`;
  $('avisos').scrollIntoView({ block: 'nearest', behavior: 'smooth' });
}

function limpiarAvisos() { $('avisos').innerHTML = ''; }

function escapar(s) {
  return String(s ?? '').replace(/[&<>"']/g, (c) =>
    ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

/* Traduce un fallo de confianza en algo accionable: decir "no puedes" sin
   decir qué hacer solo consigue que la gente se vaya. */
function explicarError(e) {
  if (e.status === 403 && e.datos.te_falta) {
    const faltan = e.datos.te_falta.map(nombreComprobacion).join(', ');
    avisar(e.datos.motivo || e.message,
      'aviso',
      `<p style="margin:8px 0 0">Te falta verificar: <b>${escapar(faltan)}</b>.</p>`);
    return;
  }
  avisar(e.message, 'error');
}

/* ---------- Sesión ---------- */

async function entrar(esAlta) {
  const email = $('email').value.trim();
  const clave = $('clave').value;
  const nombre = $('nombre').value.trim();

  if (esAlta && !nombre) { avisar('Pon un nombre para que los demás sepan con quién viajan.', 'error'); return; }

  const btn = $('btn-acceso');
  btn.disabled = true;
  try {
    const sesion = esAlta
      ? await api('POST', '/api/v1/auth/register', { name: nombre, email, password: clave })
      : await api('POST', '/api/v1/auth/login', { email, password: clave });

    estado.token = sesion.token;
    estado.usuario = sesion.user;
    localStorage.setItem('token', estado.token);
    limpiarAvisos();
    await trasEntrar();
  } catch (e) {
    avisar(e.message, 'error');
  } finally {
    btn.disabled = false;
  }
}

async function trasEntrar() {
  await Promise.all([cargarPerfil(), cargarMisTrayectos()]);
  pintar();
}

function salir() {
  estado.token = '';
  estado.usuario = null;
  estado.perfil = null;
  estado.resultados = [];
  estado.mios = [];
  localStorage.removeItem('token');
  pintar();
  dibujarMapa();
}

async function recuperarSesion() {
  if (!estado.token) return;
  try {
    estado.usuario = await api('GET', '/api/v1/me');
    await trasEntrar();
  } catch {
    // Token caducado o inválido: se empieza de cero sin molestar a nadie.
    salir();
  }
}

/* ---------- Confianza ---------- */

const COMPROBACIONES = [
  { id: 'email', nombre: 'Correo electrónico' },
  { id: 'phone', nombre: 'Teléfono' },
  { id: 'government_id', nombre: 'Documento de identidad' },
  { id: 'selfie_liveness', nombre: 'Selfie con prueba de vida' },
];

function nombreComprobacion(id) {
  const c = COMPROBACIONES.find((x) => x.id === id);
  return c ? c.nombre.toLowerCase() : id;
}

async function cargarPerfil() {
  if (!estado.usuario) return;
  estado.perfil = await api('GET', `/api/v1/users/${estado.usuario.id}/confianza`);
  estado.verificaciones = (await api('GET', '/api/v1/me/verificaciones')).verificaciones || [];
}

function comprobacionHecha(id) {
  return (estado.verificaciones || []).some(
    (v) => v.kind === id && v.status === 'verified');
}

async function verificar(id) {
  try {
    const r = await api('POST', '/api/v1/me/verificaciones', { kind: id });
    const ref = r.verificacion.provider_ref;

    // En desarrollo el proveedor no verifica nada y hay un atajo para
    // resolverlo. En producción esto devuelve 404 y hay que completar el
    // trámite en la URL del proveedor.
    try {
      await api('POST', `/api/v1/dev/verificaciones/${ref}/resolver`, { verificar: true });
    } catch {
      window.open(r.continuar_en, '_blank', 'noopener');
      avisar('Completa la verificación en la ventana que se ha abierto y vuelve aquí.');
    }
    await cargarPerfil();
    pintar();
  } catch (e) {
    avisar(e.message, 'error');
  }
}

/* ---------- Zonas y geometría ---------- */

async function cargarZonas() {
  const r = await api('GET', '/api/v1/zonas');
  estado.zonas = r.zonas;

  for (const sel of ['origen', 'destino', 'o-origen', 'o-destino']) {
    $(sel).innerHTML = estado.zonas
      .map((z, i) => `<option value="${i}">${escapar(z.nombre)}</option>`).join('');
  }
  // Un par por defecto que tenga sentido: centro → aeropuerto.
  const centro = estado.zonas.findIndex((z) => z.nombre.startsWith('Centro'));
  const aero = estado.zonas.findIndex((z) => z.nombre.includes('Aeropuerto'));
  if (centro >= 0 && aero >= 0) {
    $('origen').value = centro; $('o-origen').value = centro;
    $('destino').value = aero;  $('o-destino').value = aero;
  }
}

/* Proyección equirectangular sobre el área de servicio. A esta escala el error
   es irrelevante y mantiene reconocibles las distancias relativas.

   El lienzo se ajusta a la forma real de los datos en vez de a una caja fija:
   Austin es más alto que ancho, y forzarlo a 1000x700 dejaba medio mapa vacío. */
const MARGEN = 70;

function encuadre() {
  const lats = estado.zonas.map((z) => z.lat);
  const lngs = estado.zonas.map((z) => z.lng);
  const minLat = Math.min(...lats), maxLat = Math.max(...lats);
  const minLng = Math.min(...lngs), maxLng = Math.max(...lngs);
  const cosLat = Math.cos((minLat + maxLat) / 2 * Math.PI / 180);

  const anchoGeo = Math.max((maxLng - minLng) * cosLat, 1e-9);
  const altoGeo = Math.max(maxLat - minLat, 1e-9);
  // El lado mayor manda, para que el mapa llene el lienzo en cualquier caso.
  const escala = 760 / Math.max(anchoGeo, altoGeo);

  return {
    minLat, maxLat, minLng, cosLat, escala,
    ancho: anchoGeo * escala + 2 * MARGEN,
    alto: altoGeo * escala + 2 * MARGEN,
  };
}

function proyectar(lat, lng, e) {
  return {
    x: MARGEN + (lng - e.minLng) * e.cosLat * e.escala,
    y: MARGEN + (e.maxLat - lat) * e.escala, // la latitud crece hacia arriba
  };
}

/* nombreCorto evita que las etiquetas se pisen unas a otras. */
function nombreCorto(nombre) {
  return nombre.split(/ [(\/]/)[0].trim();
}

/* ---------- Mapa ---------- */

function dibujarMapa() {
  const svg = $('mapa');
  if (!estado.zonas.length) return;

  const e = encuadre();
  svg.setAttribute('viewBox', `0 0 ${e.ancho.toFixed(0)} ${e.alto.toFixed(0)}`);
  const capas = [];

  // Trayectos ofrecidos que ha devuelto la búsqueda.
  estado.resultados.forEach((m, i) => {
    const ruta = m.trip.route.map((p) => proyectar(p.lat, p.lng, e));
    const activo = estado.seleccionado === i;
    capas.push(`<polyline class="trazo-oferta${activo ? ' activo' : ''}"
      points="${ruta.map((p) => `${p.x.toFixed(1)},${p.y.toFixed(1)}`).join(' ')}"/>`);
  });

  // El trayecto que se está pidiendo, y el tramo que se compartiría.
  const pedido = trayectoPedido();
  if (pedido) {
    const a = proyectar(pedido.origen.lat, pedido.origen.lng, e);
    const b = proyectar(pedido.destino.lat, pedido.destino.lng, e);
    if (estado.seleccionado !== null && estado.resultados[estado.seleccionado]) {
      capas.push(`<line class="trazo-compartido" x1="${a.x}" y1="${a.y}" x2="${b.x}" y2="${b.y}"/>`);
    }
    capas.push(`<line class="trazo-mio" x1="${a.x}" y1="${a.y}" x2="${b.x}" y2="${b.y}"/>`);
  }

  // Las zonas, encima de todo para que se puedan pulsar.
  estado.zonas.forEach((z, i) => {
    const p = proyectar(z.lat, z.lng, e);
    const elegida = pedido && (i === pedido.iOrigen || i === pedido.iDestino);
    // Las etiquetas de la mitad inferior van debajo del punto: arriba se
    // solapaban con las del vecino de encima.
    const debajo = p.y > e.alto / 2;
    capas.push(`
      <g class="zona" data-zona="${i}">
        <circle class="zona-punto${elegida ? ' elegida' : ''}" cx="${p.x.toFixed(1)}" cy="${p.y.toFixed(1)}" r="${elegida ? 7 : 5}"/>
        <text class="zona-texto" x="${p.x.toFixed(1)}" y="${(p.y + (debajo ? 22 : -13)).toFixed(1)}">${escapar(nombreCorto(z.nombre))}</text>
      </g>`);
  });

  capas.push(escalaGrafica(e));
  svg.innerHTML = capas.join('');

  svg.querySelectorAll('.zona').forEach((g) => {
    g.addEventListener('click', () => elegirZona(Number(g.dataset.zona)));
  });
}

/* escalaGrafica dibuja una barra de kilómetros: sin ella el mapa esquemático no
   deja juzgar si un desvío son cinco minutos o media hora. */
function escalaGrafica(e) {
  const kmPorGrado = 111.32;
  // Se elige un número redondo que ocupe alrededor de un quinto del ancho.
  const objetivoKm = (e.ancho / e.escala) * kmPorGrado / 5;
  const km = [1, 2, 5, 10, 20, 50].reduce(
    (mejor, v) => (Math.abs(v - objetivoKm) < Math.abs(mejor - objetivoKm) ? v : mejor), 1);

  const largo = (km / kmPorGrado) * e.escala;
  const x = MARGEN, y = e.alto - 26;
  return `
    <g aria-hidden="true">
      <line x1="${x}" y1="${y}" x2="${(x + largo).toFixed(1)}" y2="${y}"
            stroke="#5d6b78" stroke-width="1.5"/>
      <line x1="${x}" y1="${y - 4}" x2="${x}" y2="${y + 4}" stroke="#5d6b78" stroke-width="1.5"/>
      <line x1="${(x + largo).toFixed(1)}" y1="${y - 4}" x2="${(x + largo).toFixed(1)}" y2="${y + 4}"
            stroke="#5d6b78" stroke-width="1.5"/>
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
  if (!estado.zonas.length) return null;
  const pre = estado.modo === 'ofrecer' ? 'o-' : '';
  const iO = Number($(pre + 'origen').value);
  const iD = Number($(pre + 'destino').value);
  if (Number.isNaN(iO) || Number.isNaN(iD) || iO === iD) return null;
  return { iOrigen: iO, iDestino: iD, origen: estado.zonas[iO], destino: estado.zonas[iD] };
}

/* ---------- Buscar ---------- */

async function buscar() {
  const t = trayectoPedido();
  if (!t) { avisar('Elige un origen y un destino distintos.', 'error'); return; }

  const desde = new Date($('cuando').value || Date.now());
  const margen = Number($('margen').value) * 60000;

  $('btn-buscar').disabled = true;
  $('resultados').innerHTML = '<p class="cargando">Buscando…</p>';
  $('resultados-caja').classList.remove('oculto');

  try {
    const r = await api('POST', '/api/v1/search', {
      pickup: { lat: t.origen.lat, lng: t.origen.lng },
      dropoff: { lat: t.destino.lat, lng: t.destino.lng },
      earliest_departure: new Date(desde.getTime() - margen).toISOString(),
      latest_departure: new Date(desde.getTime() + margen).toISOString(),
      seats: 1,
    });
    estado.resultados = r.matches || [];
    estado.seleccionado = estado.resultados.length ? 0 : null;
    limpiarAvisos();
    pintarResultados();
    dibujarMapa();
  } catch (e) {
    explicarError(e);
    $('resultados').innerHTML = '';
  } finally {
    $('btn-buscar').disabled = false;
  }
}

function pintarResultados() {
  $('resultados-titulo').textContent =
    estado.resultados.length ? `${estado.resultados.length} viaje(s) compatible(s)` : 'Sin resultados';

  if (!estado.resultados.length) {
    $('resultados').innerHTML =
      `<p class="tenue">Nadie va por ahí en esa franja todavía.
       Prueba a ampliar el margen horario, o publica tú el trayecto y espera a que alguien se sume.</p>`;
    return;
  }

  $('resultados').innerHTML = estado.resultados.map((m, i) => {
    const t = m.trip;
    const salida = new Date(t.departure_time);
    return `
      <div class="viaje ${i === estado.seleccionado ? 'activo' : ''}" data-i="${i}">
        <div class="viaje-cabecera">
          <span class="ruta">${escapar(t.origin.name)} → ${escapar(t.destination.name)}</span>
          <span class="precio">${euros(m.estimated_price_cents)}</span>
        </div>
        <div class="viaje-datos">
          <span><b>${salida.toLocaleString('es', { day: '2-digit', month: 'short', hour: '2-digit', minute: '2-digit' })}</b></span>
          <span>compartís <b>${m.shared_km.toFixed(1)} km</b></span>
          <span>a pie <b>${(m.pickup_walk_km * 1000).toFixed(0)} m</b></span>
          <span>${t.seats_available} plaza(s)</span>
        </div>
        <div class="fila" style="margin-top:10px">
          <span class="nivel nivel-${claseNivel(t.nivel_exigido)}">exige ${escapar(t.nivel_exigido)}</span>
          <button class="btn-secundario btn-fino" data-reservar="${i}">Pedir plaza</button>
        </div>
      </div>`;
  }).join('');

  $('resultados').querySelectorAll('.viaje').forEach((el) => {
    el.addEventListener('click', (ev) => {
      if (ev.target.dataset.reservar !== undefined) return;
      estado.seleccionado = Number(el.dataset.i);
      pintarResultados();
      dibujarMapa();
    });
  });
  $('resultados').querySelectorAll('[data-reservar]').forEach((b) => {
    b.addEventListener('click', () => reservar(Number(b.dataset.reservar)));
  });
}

async function reservar(i) {
  const m = estado.resultados[i];
  const t = trayectoPedido();
  try {
    await api('POST', `/api/v1/trips/${m.trip.id}/bookings`, {
      pickup: { name: t.origen.nombre, point: { lat: t.origen.lat, lng: t.origen.lng } },
      dropoff: { name: t.destino.nombre, point: { lat: t.destino.lat, lng: t.destino.lng } },
      seats: 1,
    });
    // Primero se refresca la búsqueda y después se avisa: al revés, buscar()
    // limpiaba el mensaje antes de que a nadie le diera tiempo a leerlo.
    await buscar();
    avisar('Plaza pedida. Quien organiza tiene que aceptarte antes de que sea firme.', 'bien');
  } catch (e) {
    explicarError(e);
  }
}

/* ---------- Publicar ---------- */

function actualizarSuelo() {
  const esBiplaza = $('o-vehiculo').value === 'cybercab';
  $('o-suelo').innerHTML = esBiplaza
    ? `<strong>Cybercab: viajaréis solos.</strong> Sin conductor y sin nadie más
       delante, así que ambas partes necesitáis la identidad verificada. No se
       puede rebajar.`
    : `Con 4 plazas hay más gente a bordo. El mínimo es correo y teléfono
       verificados, pero puedes exigir más.`;
}

async function publicar() {
  const t = trayectoPedido();
  if (!t) { avisar('Elige un origen y un destino distintos.', 'error'); return; }
  if (!$('o-cuando').value) { avisar('Pon la hora de salida.', 'error'); return; }

  $('btn-publicar').disabled = true;
  try {
    await api('POST', '/api/v1/trips', {
      origin: { name: t.origen.nombre, point: { lat: t.origen.lat, lng: t.origen.lng } },
      destination: { name: t.destino.nombre, point: { lat: t.destino.lat, lng: t.destino.lng } },
      departure_time: new Date($('o-cuando').value).toISOString(),
      vehicle: $('o-vehiculo').value,
      min_trust_level: $('o-nivel').value,
    });
    avisar('Trayecto publicado. Te avisaremos cuando alguien pida plaza.', 'bien');
    await cargarMisTrayectos();
    pintar();
  } catch (e) {
    explicarError(e);
  } finally {
    $('btn-publicar').disabled = false;
  }
}

/* ---------- Mis trayectos ---------- */

async function cargarMisTrayectos() {
  if (!estado.usuario) return;
  const r = await api('GET', '/api/v1/trips');
  estado.mios = (r.trips || []).filter((t) => t.host_id === estado.usuario.id);

  // Las peticiones de plaza de cada trayecto propio.
  for (const t of estado.mios) {
    try {
      const b = await api('GET', `/api/v1/trips/${t.id}/bookings`);
      t.peticiones = (b.bookings || []).filter((x) => x.status === 'pending');
    } catch { t.peticiones = []; }
  }
}

function pintarMisTrayectos() {
  if (!estado.mios.length) { $('mios-caja').classList.add('oculto'); return; }
  $('mios-caja').classList.remove('oculto');

  $('mios').innerHTML = estado.mios.map((t) => {
    const salida = new Date(t.departure_time);
    const peticiones = (t.peticiones || []).map((p) => `
      <div class="fila" style="margin-top:10px;padding-top:10px;border-top:1px solid var(--borde)">
        <span class="tenue">Alguien pide ${p.seats} plaza(s) · ${euros(p.price_cents)}</span>
        <span>
          <button class="btn-secundario btn-fino" data-aceptar="${p.id}">Aceptar</button>
          <button class="btn-secundario btn-fino" data-rechazar="${p.id}">No</button>
        </span>
      </div>`).join('');

    return `
      <div class="viaje">
        <div class="viaje-cabecera">
          <span class="ruta">${escapar(t.origin.name)} → ${escapar(t.destination.name)}</span>
          <span class="tenue mono">${escapar(t.status)}</span>
        </div>
        <div class="viaje-datos">
          <span><b>${salida.toLocaleString('es', { day: '2-digit', month: 'short', hour: '2-digit', minute: '2-digit' })}</b></span>
          <span>${t.distance_km} km</span>
          <span>${t.seats_available}/${t.seats_total} libres</span>
          <span>${escapar(t.route_source === 'osrm' ? 'ruta real' : 'ruta estimada')}</span>
        </div>
        ${peticiones}
      </div>`;
  }).join('');

  $('mios').querySelectorAll('[data-aceptar]').forEach((b) =>
    b.addEventListener('click', () => decidir(b.dataset.aceptar, true)));
  $('mios').querySelectorAll('[data-rechazar]').forEach((b) =>
    b.addEventListener('click', () => decidir(b.dataset.rechazar, false)));
}

/* Aceptar a alguien obliga a asumir la responsabilidad que imponen los términos
   del robotaxi. No se puede aceptar sin verlo. */
async function decidir(id, aceptar) {
  if (aceptar) {
    const ok = confirm(
      'Los términos del robotaxi te hacen responsable de la conducta de quien ' +
      'dejes subir al vehículo, incluidos los daños que cause.\n\n' +
      'Revisa su perfil de confianza antes de aceptar.\n\n¿Lo asumes?');
    if (!ok) return;
  }
  try {
    await api('POST', `/api/v1/bookings/${id}/decision`,
      { accept: aceptar, acepta_responsabilidad: aceptar });
    avisar(aceptar ? 'Plaza confirmada.' : 'Petición rechazada.', 'bien');
    await cargarMisTrayectos();
    pintarMisTrayectos();
  } catch (e) {
    explicarError(e);
  }
}

/* ---------- Pintado general ---------- */

function claseNivel(n) {
  return ({ 'nuevo': 'nuevo', 'básico': 'basico', 'verificado': 'verificado', 'veterano': 'veterano' })[n] || 'nuevo';
}

function euros(cents) { return (cents / 100).toFixed(2) + ' $'; }

function pintar() {
  const dentro = !!estado.usuario;

  $('acceso').classList.toggle('oculto', dentro);
  $('confianza').classList.toggle('oculto', !dentro);
  $('modo').classList.toggle('oculto', !dentro);
  $('buscar').classList.toggle('oculto', !dentro || estado.modo !== 'buscar');
  $('ofrecer').classList.toggle('oculto', !dentro || estado.modo !== 'ofrecer');

  $('sesion').innerHTML = dentro
    ? `<span class="nivel nivel-${claseNivel(estado.perfil?.nivel)}">${escapar(estado.perfil?.nivel || 'nuevo')}</span>
       <span class="tenue">${escapar(estado.usuario.name)}</span>
       <button class="enlace" id="btn-salir">Salir</button>`
    : '';
  if (dentro) $('btn-salir').addEventListener('click', salir);

  if (dentro) pintarConfianza();
  pintarMisTrayectos();
}

function pintarConfianza() {
  const nivel = estado.perfil?.nivel || 'nuevo';
  $('mi-nivel').textContent = nivel;
  $('mi-nivel').className = 'nivel nivel-' + claseNivel(nivel);

  const verificado = nivel === 'verificado' || nivel === 'veterano';
  $('confianza-texto').innerHTML = verificado
    ? 'Identidad acreditada. Puedes compartir cualquier vehículo, incluido el Cybercab biplaza.'
    : 'Aquí no hay conductor que haga de testigo. Por eso hace falta acreditar ' +
      'quién eres antes de compartir coche con desconocidos — y por eso puedes ' +
      'fiarte de quien se suba contigo.';

  $('lista-comprobaciones').innerHTML = COMPROBACIONES.map((c) => {
    const hecha = comprobacionHecha(c.id);
    return `<li class="${hecha ? 'hecha' : ''}">
      <span>${escapar(c.nombre)}</span>
      ${hecha
        ? '<span class="marca-ok">✓</span>'
        : `<button class="btn-secundario btn-fino" data-verificar="${c.id}">Verificar</button>`}
    </li>`;
  }).join('');

  $('lista-comprobaciones').querySelectorAll('[data-verificar]').forEach((b) =>
    b.addEventListener('click', () => verificar(b.dataset.verificar)));
}

/* ---------- Arranque ---------- */

function conectarEventos() {
  let esAlta = false;
  const cambiarAcceso = (alta) => {
    esAlta = alta;
    $('tab-entrar').setAttribute('aria-selected', String(!alta));
    $('tab-alta').setAttribute('aria-selected', String(alta));
    $('campo-nombre').hidden = !alta;
    $('btn-acceso').textContent = alta ? 'Crear cuenta' : 'Entrar';
    $('clave').autocomplete = alta ? 'new-password' : 'current-password';
  };
  $('tab-entrar').addEventListener('click', () => cambiarAcceso(false));
  $('tab-alta').addEventListener('click', () => cambiarAcceso(true));
  $('form-acceso').addEventListener('submit', (e) => { e.preventDefault(); entrar(esAlta); });

  const cambiarModo = (modo) => {
    estado.modo = modo;
    $('tab-buscar').setAttribute('aria-selected', String(modo === 'buscar'));
    $('tab-ofrecer').setAttribute('aria-selected', String(modo === 'ofrecer'));
    siguienteEsDestino = false;
    pintar();
    dibujarMapa();
  };
  $('tab-buscar').addEventListener('click', () => cambiarModo('buscar'));
  $('tab-ofrecer').addEventListener('click', () => cambiarModo('ofrecer'));

  $('btn-buscar').addEventListener('click', buscar);
  $('btn-publicar').addEventListener('click', publicar);
  $('o-vehiculo').addEventListener('change', actualizarSuelo);

  for (const id of ['origen', 'destino', 'o-origen', 'o-destino']) {
    $(id).addEventListener('change', dibujarMapa);
  }
}

function horaPorDefecto() {
  const d = new Date(Date.now() + 2 * 3600 * 1000);
  d.setMinutes(0, 0, 0);
  const local = new Date(d.getTime() - d.getTimezoneOffset() * 60000);
  return local.toISOString().slice(0, 16);
}

async function arrancar() {
  conectarEventos();
  $('cuando').value = horaPorDefecto();
  $('o-cuando').value = horaPorDefecto();

  try {
    await cargarZonas();
  } catch {
    avisar('No se pudo cargar el área de servicio. Recarga la página.', 'error');
    return;
  }
  actualizarSuelo();
  await recuperarSesion();
  pintar();
  dibujarMapa();
}

arrancar();
