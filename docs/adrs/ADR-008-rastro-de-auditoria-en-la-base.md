---
id: ADR-008
title: El rastro de auditoría vive en la base de datos, y por defecto
status: accepted
date: 2026-09-14
deciders: jcsvwinston
related: [ADR-004, ADR-007]
supersedes: null
tags: [orbit, auditoría, retención, almacenamiento]
---

# ADR-008 — El rastro de auditoría vive en la base, y por defecto

## Contexto

El rastro del panel era un **anillo en memoria**: cubría todas las superficies
mutantes, guardaba los dos lados de cada edición y **no sobrevivía al
proceso**. Un despliegue, un reinicio o una segunda réplica lo dejaban vacío.

Lo peor no era la pérdida, era el **silencio**: el panel seguía enseñando un
rastro, y el rastro simplemente empezaba cuando empezaba el proceso. Nada en
la pantalla distinguía «no pasó nada» de «esto arrancó hace diez minutos». La
medición `S0` del arco A6 lo registró como `AUD-05` ausente, con `AUD-06`
(export), `AUD-07` (retención) y `DS-16` (historial de un registro) detrás:
las tres son inalcanzables sin lo primero.

## Decisión

**El rastro se escribe en una tabla que el panel crea y posee
(`nucleus_admin_audit`), en la misma base contra la que ya autentica, y es el
comportamiento POR DEFECTO cuando la aplicación tiene base de datos.**

1. **Por defecto, no opcional.** Un rastro que hay que encender es un rastro
   que nadie tiene el día que lo necesita. El precedente ya existía: el panel
   crea `nucleus_admin_users` sin pedir permiso (ADR-004). `audit_store:
   memory` vuelve al anillo para quien lo prefiera.

2. **Degradar antes que no arrancar.** Sin handle de base, o si la tabla no se
   puede crear, el panel avisa y sigue con el anillo. Un panel de
   administración que se niega a arrancar es peor que uno cuyo rastro no es
   durable.

3. **Una escritura de auditoría nunca tumba la operación que documenta.** Un
   INSERT que falla se registra en el log de la aplicación y la entrada se
   queda en un anillo pequeño en memoria, para que el operador vea el rastro
   degradado en vez de un rastro silencioso.

4. **La retención es un PERÍODO** (`audit_retention_days`), porque eso es lo
   que es una ventana de cumplimiento; `audit_max_size` es un recuento y
   responde a otra pregunta (y sólo acota el anillo). Se aplica al montar y
   como mucho una vez por hora **en la ruta de escritura**: un barredor en
   segundo plano sería una goroutine más que arrancar, parar y perder, y un
   panel que no registra nada no necesita barrido.

5. **La ventana se puede declarar desde el panel** y se aplica al instante,
   pero NO reescribe la configuración de la aplicación: un reinicio vuelve al
   valor configurado, y el payload lo dice (`configured_retention_days`) en
   vez de dejar que el operador crea otra cosa.

6. **El export es un fichero** (`?format=csv`) con los mismos filtros que el
   listado, y **queda registrado** (`audit.export`): quién se llevó una copia
   del log es justo la clase de cosa para la que existe el log.

7. **El historial de un registro es el mismo rastro leído por fila**, no un
   segundo almacén ni un versionado. Lo gobierna el permiso del REGISTRO
   (`retrieve`), no `audit_view`, y hereda el alcance por fila y los permisos
   por campo de ese permiso (ADR-007): un historial que enseñara la fila que
   el operador no puede abrir, o el campo que no puede leer, sería el rodeo de
   los dos.

8. **El panel DICE lo que sirve.** `persistent` y `retention_days` viajan en
   el listado y en el historial, para que un hueco se lea como lo que es.

## Consecuencias

- **Una aplicación que actualiza gana una tabla.** Se crea idempotentemente en
  los cinco dialectos, con la clave generada por la BASE (identity/serial/
  autoincrement según dialecto) para que dos réplicas no colisionen — a
  diferencia de `nucleus_admin_users`, cuyo id escribe el panel.
- **Una escritura de auditoría es ahora un INSERT.** El panel audita cada
  operación mutante, así que cada una paga una escritura más. A cambio, el
  rastro deja de desaparecer.
- **El histórico anterior no se recupera.** Lo que había en el anillo de un
  proceso vivo se pierde al desplegar esta versión, como se perdía antes en
  cada despliegue.
- **El límite conocido**: la entrada se escribe **después** del cambio y en su
  propia transacción, así que un fallo entre ambos deja el cambio sin entrada.
  Un rastro transaccional es otra cosa y vive en la capa de datos (el
  `quark_audit` de Quark, escrito en la misma transacción); la doc pública lo
  dice en vez de dejarlo suponer.
