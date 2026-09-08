package domain

// VersionTerminos identifica la redacción vigente de las condiciones y de la
// política de privacidad.
//
// Es una fecha y no un número porque es lo que se puede contrastar con el texto
// publicado: quien reclame algo dentro de dos años tiene derecho a saber qué
// aceptó exactamente, y para eso la versión tiene que apuntar a un documento
// concreto y no a "la versión 3".
//
// Al cambiarla hay que publicar el texto nuevo en la interfaz y dejar el
// anterior accesible: quien aceptó el viejo aceptó el viejo.
const VersionTerminos = "2026-09-08"
