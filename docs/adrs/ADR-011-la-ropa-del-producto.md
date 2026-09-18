---
id: ADR-011
title: El panel lleva la ropa del producto — marca, tarjetas propias e idioma, sin traducir los datos
status: accepted
date: 2026-09-18
deciders: jcsvwinston
related: [ADR-004, ADR-010]
supersedes: null
tags: [orbit, personalizacion, marca, i18n, dashboard]
---

# ADR-011 — Marca, tarjetas propias e idioma

## Contexto

`ADR-010` dejó a la aplicación añadir sus verbos y sus pantallas. Quedaba lo
que el operador ve ANTES de usar ninguna de las dos: un panel que se presenta
con el nombre del producto y nada más suyo, que abre en una pantalla fija con
los números que Orbit sabe contar, y que habla inglés aunque el equipo que lo
usa —y el producto que administra— no lo hagan.

La medición `S0` del arco A6 lo registró como `CUST-02` (marca: logo, color,
favicon), `CUST-03` (tablero con widgets declarables) y `CUST-05` (el panel
habla otro idioma). Eran las tres últimas ausencias del banco.

## Decisión

1. **Marca: tres cadenas en la superficie de montaje**
   (`orbit.Config.Branding`), y por ser cadenas **sí se enlazan desde
   `nucleus.yml`**, a diferencia de las acciones y las pantallas. Viajan al
   documento como `<meta>`, el mismo canal que el prefijo y el título — que
   es lo que las hace estar en la **pantalla de login**, la única que se ve
   antes de ser nadie.

2. **Los valores se VALIDAN, no se escapan.** Un logo `javascript:` sería
   ejecución de script en cada página del panel concedida por una línea de
   YAML, y un «color» que fuese un fragmento de hoja de estilos, lo mismo.
   Una URL es absoluta `http(s)` o una ruta del propio sitio; un color es
   hexadecimal; cualquier otra cosa **impide arrancar**. Escapar habría
   dejado el valor inerte en el documento y vivo para lo siguiente que lo
   leyera.

3. **El color de marca decide el texto que va encima.** Se calcula desde la
   luminosidad, porque una marca se elige para parecer una marca y no para
   contrastar con el blanco: texto blanco sobre un amarillo claro es un fallo
   de contraste que el panel habría introducido **en nombre de la
   aplicación**. Mismo criterio para el anillo de foco, que sigue al color.

4. **Las tarjetas del tablero las declara la aplicación**
   (`orbit.Config.Widgets`), con la función que lee el valor. El panel pone
   la pantalla, la autorización (`admin:dashboard`, con el permiso por
   tarjeta, para que «este rol ve las cifras de finanzas» sea una política y
   no un fork), un límite de tres segundos por tarjeta y la degradación.

5. **Una tarjeta que falla se dibuja diciéndolo.** Quitarla contaría una
   consulta rota como «no hay nada que ver», y esperarla haría que el
   resumen entero pareciese roto en vez de la tarjeta. Un `panic` dentro de
   una tarjeta es el error de esa tarjeta, no del proceso que sirve el panel.

6. **El valor es una CADENA.** La aplicación sabe formatear sus números; un
   panel que los formateara tendría que ser informado de la moneda, el locale
   y la precisión para equivocarse de tres maneras.

7. **El idioma cubre el CROMO y nunca los datos.** El panel trae sus propias
   frases en inglés y español; `Config.Locale` elige con cuál abre (y fija el
   `lang` del documento, que es con lo que un lector de pantalla lo
   pronuncia) y `Config.Messages` añade o sustituye cualquier frase, incluso
   para un idioma que el panel no trae. Un modelo llamado `Invoice` se llama
   Invoice en todos los idiomas, la etiqueta de un campo sale de las
   etiquetas de struct de la aplicación y un error que devuelve la aplicación
   es su frase.

8. **Los catálogos se FUNDEN, no se eligen**: la frase de la aplicación gana
   a la traducción del panel, y ésta al inglés del panel. Así una corrección
   de cinco palabras son cinco palabras, una traducción a medias sigue siendo
   legible, y una clave sin traducir **se lee en inglés y no como
   `nav.audit`**.

9. **El catálogo se sirve sin sesión** (`<prefijo>/ui/messages.json`), junto
   a los activos ya públicos: el login se dibuja antes de que haya sesión, y
   una página de entrada en el idioma equivocado sería lo primero que vería
   el operador. El contenido es el cromo del propio panel —las mismas cadenas
   que ya viajan en el bundle— y no lleva dato alguno de la aplicación.

## Consecuencias

- **Adición al contrato congelado**: `orbit.Config.Branding`, `Widgets`,
  `Locale` y `Messages`, más los tipos `Branding`, `Widget`, `WidgetValue` y
  `WidgetItem`. Ninguna firma existente cambia (QADR-0010).
- **El panel deja de tener un solo color**: el tema oscuro y el claro
  comparten la propiedad `--primary`, así que una marca declarada se aplica a
  los dos. Un par de colores por tema sería otra decisión, y ninguna
  aplicación la ha pedido.
- **Lo que NO hace**: no hay hoja de estilos propia ni plantillas
  sustituibles —eso convierte la interfaz del panel en una API, y cada
  cambio de maquetación en una rotura de contrato—; ni un selector de idioma
  por operador, porque el idioma hoy es una propiedad del despliegue y no una
  preferencia con dónde guardarse; ni traducción de las pantallas más
  profundas más allá de las frases del catálogo, que es exactamente lo que el
  catálogo enumera.
