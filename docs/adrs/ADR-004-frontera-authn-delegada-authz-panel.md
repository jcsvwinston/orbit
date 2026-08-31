---
id: ADR-004
title: Autenticación delegada, autorización del panel propia
status: accepted
date: 2026-08-31
deciders: jcsvwinston
related: [nucleus/ADR-019]
supersedes: null
tags: [orbit, seguridad, autenticacion, autorizacion]
---

# ADR-004 — Autenticación delegada ≠ autorización del panel

> **ADR retroactivo.** La frontera se implementó al integrar el panel con
> la cadena de autenticación del framework (`auth_backends`) y está
> descrita en la doc pública; este acta la fija como decisión para que no
> se reabra por accidente.

## Contexto

Nucleus permite a la app declarar una cadena de autenticación
(`auth_backends: [ldap, local]`). Si Orbit la usa tal cual para decidir
quién entra al panel, conectar un directorio corporativo convierte en
silencio a **cada empleado de la empresa** en administrador del panel. Si
la ignora, el operador mantiene credenciales duplicadas y una fila local
obsoleta sobrevive a la revocación en el directorio.

## Decisión

Las dos preguntas se separan y las responden partes distintas:

- **Autenticación (¿de quién son las credenciales?) — delegada.** Con
  `auth_backends` configurado, el panel valida la password contra la
  cadena del framework. Orbit no lleva cliente LDAP ni conoce backends:
  pregunta a la cadena.
- **Autorización de entrada (¿puede esa persona administrar?) — propia.**
  La tabla de administradores del panel (`nucleus_admin_users`) decide
  quién entra. Una cuenta del directorio sin fila de admin es rechazada.

Las dos condiciones son **conjuntivas**: una fila local no es un bypass de
la cadena (una cuenta revocada en el directorio no entra por tener fila), y
la cadena no es un bypass de la tabla. Sin `auth_backends`, el panel valida
contra su propia tabla, como siempre.

Dentro del panel, cada acción pasa además por el authorizer del host
(RBAC), pero eso es la autorización por-acción de siempre; este ADR fija la
frontera de **entrada**.

## Consecuencias

- Conectar un directorio no amplía la población de administradores; solo
  cambia quién verifica la password.
- Alta de admins = insertar la fila (bootstrap o `nucleus createuser`);
  baja efectiva = revocar en el directorio **o** borrar la fila, cualquiera
  de las dos corta el acceso.
- El operador de un directorio no puede autoproclamarse admin del panel; el
  dueño de la base de admins no puede saltarse la password del directorio.
  Cualquier bypass de una de las dos patas es una vulnerabilidad
  (SECURITY.md, «What is in scope»).
