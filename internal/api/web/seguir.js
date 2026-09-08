'use strict';

/* Seguimiento de un viaje.
 *
 * Lo abre alguien que no usa esta app y que puede estar asustado. Se refresca
 * solo cada medio minuto porque la posición cambia mientras el coche anda, y no
 * pide nada a quien mira: ni cuenta, ni permisos, ni un botón que pulsar. */

const TEXTOS = {
  en: {
    'cargando': 'Loading the trip…',
    'alerta.titulo': 'Emergency button pressed',
    'alerta.cuando': '{quien} pressed it {cuando}.',
    'alerta.llamar': 'Call emergency services',
    'alerta.aviso': 'This app cannot call for you. If you believe they are in danger, make the call yourself — you have the vehicle and the location below.',
    'titulo': '{quien} is going to {destino}',
    'sub': 'From {origen}. This page updates on its own.',
    'mapa.alt': 'The route of the trip, with the last known position',
    'dato.salida': 'Departure',
    'dato.vehiculo': 'Vehicle',
    'dato.duracion': 'Estimated time',
    'dato.estado': 'Status',
    'duracion.min': '{n} min',
    'estado.open': 'open',
    'estado.full': 'full',
    'estado.cancelled': 'cancelled',
    'estado.completed': 'finished',
    'ocupantes.titulo': 'Who is in the vehicle',
    'papel.organiza': 'organiser',
    'papel.pasajero': 'passenger',
    'nivel.nuevo': 'new',
    'nivel.básico': 'basic',
    'nivel.verificado': 'verified',
    'nivel.veterano': 'veteran',
    'posicion': 'Last known position: {cuando}.',
    'sinposicion': 'No position reported. The route below is the one the vehicle is following.',
    'caduca': 'This link stops working on {cuando}.',
    'caducado.titulo': 'This link no longer follows a trip',
    'caducado.texto': 'Either the trip is over, or whoever shared it closed the link. If you are worried about them, call them.',
    'caducado.volver': 'What is Cybercab Go Share?',
  },
  es: {
    'cargando': 'Cargando el viaje…',
    'alerta.titulo': 'Han pulsado el botón de emergencia',
    'alerta.cuando': '{quien} lo pulsó {cuando}.',
    'alerta.llamar': 'Llamar a emergencias',
    'alerta.aviso': 'Esta app no puede llamar por ti. Si crees que está en peligro, haz tú la llamada: abajo tienes el vehículo y la ubicación.',
    'titulo': '{quien} va a {destino}',
    'sub': 'Desde {origen}. Esta página se actualiza sola.',
    'mapa.alt': 'La ruta del viaje, con la última posición conocida',
    'dato.salida': 'Salida',
    'dato.vehiculo': 'Vehículo',
    'dato.duracion': 'Tiempo estimado',
    'dato.estado': 'Estado',
    'duracion.min': '{n} min',
    'estado.open': 'abierto',
    'estado.full': 'completo',
    'estado.cancelled': 'anulado',
    'estado.completed': 'terminado',
    'ocupantes.titulo': 'Quién va en el vehículo',
    'papel.organiza': 'organiza',
    'papel.pasajero': 'pasajero',
    'nivel.nuevo': 'nuevo',
    'nivel.básico': 'básico',
    'nivel.verificado': 'verificado',
    'nivel.veterano': 'veterano',
    'posicion': 'Última posición conocida: {cuando}.',
    'sinposicion': 'No ha mandado su posición. La ruta de abajo es la que sigue el vehículo.',
    'caduca': 'Este enlace deja de funcionar el {cuando}.',
    'caducado.titulo': 'Este enlace ya no sigue ningún viaje',
    'caducado.texto': 'O el viaje ha terminado, o quien lo compartió cerró el enlace. Si te preocupa, llámale.',
    'caducado.volver': '¿Qué es Cybercab Go Share?',
  },
};

let idioma = elegirIdioma();
let ultimaVista = null;

function elegirIdioma() {
  const url = new URLSearchParams(location.search).get('lang');
  if (TEXTOS[url]) return url;
  try {
    const guardado = localStorage.getItem('idioma');
    if (TEXTOS[guardado]) return guardado;
  } catch { /* almacenamiento bloqueado: seguimos con el del navegador */ }
  return (navigator.language || 'en').slice(0, 2) === 'es' ? 'es' : 'en';
}

function t(clave, vars) {
  let texto = TEXTOS[idioma][clave] ?? TEXTOS.en[clave] ?? clave;
  if (vars) for (const [k, v] of Object.entries(vars)) texto = texto.replaceAll(`{${k}}`, v);
  return texto;
}

const $ = (id) => document.getElementById(id);

function escapar(s) {
  return String(s ?? '').replace(/[&<>"']/g, (c) =>
    ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

function aplicarTextos() {
  document.querySelectorAll('[data-t]').forEach((el) => { el.textContent = t(el.dataset.t); });
  document.documentElement.lang = idioma;
  document.querySelectorAll('.idiomas button').forEach((b) =>
    b.setAttribute('aria-pressed', String(b.dataset.idioma === idioma)));
}

function fecha(d) {
  return d.toLocaleString(idioma, {
    weekday: 'short', day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit',
  });
}

/* haceCuanto dice "hace 3 minutos" y no una hora exacta: en una emergencia lo
   que importa no es a qué hora fue, es cuánto hace. */
function haceCuanto(d) {
  const seg = Math.round((Date.now() - d.getTime()) / 1000);
  const rel = new Intl.RelativeTimeFormat(idioma, { numeric: 'auto' });
  if (seg < 60) return rel.format(-seg, 'second');
  if (seg < 3600) return rel.format(-Math.round(seg / 60), 'minute');
  if (seg < 86400) return rel.format(-Math.round(seg / 3600), 'hour');
  return rel.format(-Math.round(seg / 86400), 'day');
}

function claseNivel(n) {
  return { 'básico': 'basico', 'verificado': 'verificado', 'veterano': 'veterano' }[n] || 'nuevo';
}

/* ============ El mapa ============ */

/* Esquemático, como el de la aplicación: lo que hay que ver es por dónde va la
   ruta y dónde está el coche dentro de ella, no el nombre de las calles. */
function dibujarMapa(vista) {
  const svg = $('mapa');
  const W = 600, H = 320, M = 34;
  const puntos = vista.ruta.slice();
  if (vista.ultima_posicion) puntos.push(vista.ultima_posicion);
  if (puntos.length < 2) return;

  const lats = puntos.map((p) => p.lat), lngs = puntos.map((p) => p.lng);
  const minLat = Math.min(...lats), maxLat = Math.max(...lats);
  const minLng = Math.min(...lngs), maxLng = Math.max(...lngs);
  // Un margen mínimo para que un viaje muy corto no salga como una mancha.
  const anchoLng = Math.max(maxLng - minLng, 0.004);
  const altoLat = Math.max(maxLat - minLat, 0.004);
  const escala = Math.min((W - 2 * M) / anchoLng, (H - 2 * M) / altoLat);
  const cx = (minLng + maxLng) / 2, cy = (minLat + maxLat) / 2;

  const px = (p) => W / 2 + (p.lng - cx) * escala;
  const py = (p) => H / 2 - (p.lat - cy) * escala;

  const linea = vista.ruta.map((p) => `${px(p).toFixed(1)},${py(p).toFixed(1)}`).join(' ');
  const a = vista.ruta[0], b = vista.ruta[vista.ruta.length - 1];

  let coche = '';
  if (vista.ultima_posicion) {
    const p = vista.ultima_posicion;
    coche = `<circle cx="${px(p)}" cy="${py(p)}" r="11" fill="#3ddc97" opacity=".2"/>
             <circle cx="${px(p)}" cy="${py(p)}" r="5.5" fill="#3ddc97"/>`;
  }

  svg.innerHTML = `<title>${escapar(t('mapa.alt'))}</title>
    <polyline points="${linea}" fill="none" stroke="#2fd4e8" stroke-width="2.5"
              stroke-linecap="round" stroke-linejoin="round"/>
    <circle cx="${px(a)}" cy="${py(a)}" r="5" fill="#0e1216" stroke="#2fd4e8" stroke-width="2"/>
    <circle cx="${px(b)}" cy="${py(b)}" r="5" fill="#2fd4e8"/>
    <text x="${px(a)}" y="${py(a) - 12}" fill="#9aa9b7" font-size="11"
          font-family="ui-monospace, monospace" text-anchor="middle">${escapar(vista.origen.name)}</text>
    <text x="${px(b)}" y="${py(b) + 22}" fill="#eef3f7" font-size="11"
          font-family="ui-monospace, monospace" text-anchor="middle">${escapar(vista.destino.name)}</text>
    ${coche}`;
}

/* ============ Pintar ============ */

function pintar(vista) {
  ultimaVista = vista;
  aplicarTextos();
  $('cargando').classList.add('oculto');
  $('caducado').classList.add('oculto');
  $('viaje').classList.remove('oculto');

  $('titulo').textContent = t('titulo', { quien: vista.comparte, destino: vista.destino.name });
  $('sub').textContent = t('sub', { origen: vista.origen.name });

  $('d-salida').textContent = fecha(new Date(vista.salida));
  $('d-vehiculo').textContent = vista.vehiculo === 'cybercab' ? 'Cybercab' : 'Model Y';
  $('d-duracion').textContent = t('duracion.min', { n: Math.round(vista.duracion_min) });
  $('d-estado').textContent = t('estado.' + vista.estado);

  $('ocupantes').innerHTML = (vista.ocupantes || []).map((o) => `
    <li><b>${escapar(o.nombre)}</b>
      <span class="nivel n-${claseNivel(o.nivel)}">${escapar(t('nivel.' + o.nivel))}</span>
      <span class="papel">${escapar(t('papel.' + o.papel))}</span></li>`).join('');

  const pos = $('posicion');
  pos.classList.remove('oculto');
  pos.textContent = vista.ultima_posicion_at
    ? t('posicion', { cuando: haceCuanto(new Date(vista.ultima_posicion_at)) })
    : t('sinposicion');
  if (!vista.ultima_posicion_at) pos.style.color = 'var(--tenue)';

  $('caduca').textContent = t('caduca', { cuando: fecha(new Date(vista.expira)) });
  dibujarMapa(vista);

  const alerta = $('alerta');
  alerta.classList.toggle('oculto', !vista.alerta);
  if (vista.alerta) {
    $('alerta-cuando').textContent = t('alerta.cuando', {
      quien: vista.comparte, cuando: haceCuanto(new Date(vista.alerta.created_at)),
    });
    const nota = $('alerta-nota');
    nota.classList.toggle('oculto', !vista.alerta.nota);
    nota.textContent = vista.alerta.nota || '';
    // El número sale del servidor: a qué número llamar es un dato del
    // servicio, no una constante escrita en esta página.
    $('telefono').textContent = vista.telefono_emergencias;
    $('btn-911').href = 'tel:' + vista.telefono_emergencias;
    document.title = t('alerta.titulo') + ' · Cybercab Go Share';
  }
}

function pintarCaducado() {
  aplicarTextos();
  $('cargando').classList.add('oculto');
  $('viaje').classList.add('oculto');
  $('alerta').classList.add('oculto');
  $('caducado').classList.remove('oculto');
}

async function cargar() {
  const testigo = new URLSearchParams(location.search).get('t');
  if (!testigo) { pintarCaducado(); return; }
  try {
    const resp = await fetch('/api/v1/seguimiento?t=' + encodeURIComponent(testigo));
    if (!resp.ok) { pintarCaducado(); return; }
    pintar(await resp.json());
  } catch {
    // Sin red no se borra lo que ya se veía: quedarse con los datos de hace un
    // minuto es mejor que quedarse con una pantalla vacía.
    if (!ultimaVista) pintarCaducado();
  }
}

document.querySelectorAll('.idiomas button').forEach((b) =>
  b.addEventListener('click', () => {
    idioma = b.dataset.idioma;
    try { localStorage.setItem('idioma', idioma); } catch { /* ídem */ }
    if (ultimaVista) pintar(ultimaVista); else aplicarTextos();
  }));

aplicarTextos();
cargar();
// La posición cambia mientras el coche anda: media hora mirando una página
// congelada no es seguir a nadie.
setInterval(cargar, 30000);
