package routing

import "errors"

// ErrRutaInsuficiente se devuelve cuando faltan puntos para trazar una ruta.
var ErrRutaInsuficiente = errors.New("hacen falta al menos dos puntos para trazar una ruta")

// ErrSinRuta se devuelve cuando el proveedor no encuentra camino entre los
// puntos: por ejemplo, un punto en mitad de un lago.
var ErrSinRuta = errors.New("no hay ruta por carretera entre esos puntos")
