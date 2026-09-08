/* Elige el idioma de la página legal.
 *
 * Sale del mismo sitio que el de la aplicación (localStorage) para que abrir
 * las condiciones desde el formulario de alta no cambie de idioma a mitad. El
 * parámetro ?lang manda sobre todo lo demás: es lo que permite enlazar una
 * redacción concreta en un correo o en un expediente. */
(function () {
  var IDIOMAS = ['es', 'en'];

  function elegido() {
    var url = new URLSearchParams(location.search).get('lang');
    if (IDIOMAS.indexOf(url) >= 0) return url;
    try {
      var guardado = localStorage.getItem('idioma');
      if (IDIOMAS.indexOf(guardado) >= 0) return guardado;
    } catch (e) { /* almacenamiento bloqueado: seguimos con el idioma del navegador */ }
    return (navigator.language || 'en').slice(0, 2) === 'es' ? 'es' : 'en';
  }

  function aplicar(idioma) {
    document.documentElement.lang = idioma;
    document.querySelectorAll('article[data-idioma]').forEach(function (bloque) {
      bloque.classList.toggle('visible', bloque.dataset.idioma === idioma);
    });
    document.querySelectorAll('.idiomas button').forEach(function (b) {
      b.setAttribute('aria-pressed', String(b.dataset.idioma === idioma));
    });
    var titulo = document.querySelector('article[data-idioma].visible h1');
    if (titulo) document.title = titulo.textContent + ' · Cybercab Go Share';
  }

  document.querySelectorAll('.idiomas button').forEach(function (b) {
    b.addEventListener('click', function () {
      try { localStorage.setItem('idioma', b.dataset.idioma); } catch (e) { /* ídem */ }
      aplicar(b.dataset.idioma);
    });
  });

  aplicar(elegido());
})();
