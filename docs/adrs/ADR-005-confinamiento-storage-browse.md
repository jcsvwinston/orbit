---
id: ADR-005
title: El navegador de storage del panel se confina a un root fijo
status: accepted
date: 2026-08-31
deciders: jcsvwinston
related: [ADR-004]
supersedes: null
tags: [orbit, seguridad, storage, panel]
---

# ADR-005 — Confinamiento del navegador de storage a `uploads/`

> **ADR retroactivo.** La decisión se ejecutó al cerrar el hallazgo de la
> deuda post-anomalías (la rama del store llegaba a `store.List` sin pasar
> por la normalización); este acta fija el porqué para que nadie «amplíe»
> el navegador quitando la puerta.

## Contexto

El panel incluye un navegador de la capa de storage del host. La capa
tiene dos backends: un `storage.Store` configurado (bucket S3-like) o el
filesystem local. El path a listar lo aporta **el navegador del
operador** (query string), y un panel de admin no es excusa para confiar
en él: un rol de solo-lectura de storage (`storage_view`) no debe poder
enumerar el bucket entero ni escapar por `../` al filesystem del proceso.

Se encontró exactamente eso: `normalizeStorageBrowsePath` existía y estaba
testeada, pero la rama del store no la llamaba — `store.List` recibía la
query cruda y cualquier prefijo del bucket era enumerable.

## Decisión

Todo listado del navegador se **confina al root fijo `uploads/`**
(`adminStorageBrowseRoot`), en las dos ramas y **antes** de tocar el
backend:

- La entrada se normaliza (`path.Clean`, separadores unificados); vacío o
  `/` colapsan al root.
- Un path fuera de `uploads/` (incluido cualquier `../` que escape) se
  rechaza con «access denied», no se corrige en silencio hacia dentro
  cuando la intención era salir.
- La rama de filesystem añade su propio cinturón `Abs`+`HasPrefix` sobre el
  root resuelto.

El root no es configurable a propósito: hacerlo configurable convertiría un
valor de config (algo que un rol con permisos de config puede tocar) en una
ampliación del alcance de lectura de todos los roles de storage.

## Consecuencias

- `storage_view` significa «ver lo que la app sube», no «ver el bucket»:
  el navegador enseña `uploads/` y nada más, aunque el store contenga
  otros prefijos.
- El test de contrato (`storage_browse_confine_test.go`) pinta la regla
  con un store espía: ningún path del operador alcanza el backend sin
  confinar. Ese test es la definición ejecutable de este ADR.
- Si algún día hace falta navegar otro prefijo, el camino es una decisión
  sucesora que piense el modelo de permisos, no un parámetro más.
