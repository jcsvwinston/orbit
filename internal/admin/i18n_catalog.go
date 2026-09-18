// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

// The panel's own phrases, in the languages it ships.
//
// This is the CHROME and only the chrome: navigation, the login screen, the
// verbs on buttons, the words a screen says when it has nothing to show. It
// stops where the application begins — a model called Invoice is called
// Invoice in every language, a field label comes from the application's
// struct tags, and an error an application returns is the application's own
// sentence. Translating those would mean translating the product's data,
// which the panel has no business doing.
//
// The keys are the contract between this file and the interface, and the
// English catalogue is the fallback for every other: a phrase nobody
// translated reads in English rather than as its key.
//
// An application adds a language, or overrides a phrase, through
// orbit.Config.Messages — including for a language not shipped here, so this
// map is not a list of languages anyone has to be blessed into.
var panelMessages = map[string]map[string]string{
	defaultLocale: {
		"nav.overview":          "Overview",
		"nav.data_studio":       "Data Studio",
		"nav.system":            "System Pulse",
		"nav.live":              "Network Inspector",
		"nav.sessions":          "Sessions",
		"nav.health":            "Health",
		"nav.operators":         "Operators",
		"nav.rbac":              "Access Control",
		"nav.audit":             "Audit Log",
		"nav.main":              "Main navigation",
		"nav.open":              "Open navigation",
		"nav.close":             "Close navigation",
		"nav.expand":            "Expand sidebar",
		"nav.collapse":          "Collapse sidebar",
		"session.signed_in":     "Signed in",
		"session.active":        "Admin session active",
		"session.sign_out":      "Sign out",
		"theme.light":           "Switch to light theme",
		"theme.dark":            "Switch to dark theme",
		"login.title":           "Sign in",
		"login.username":        "Username",
		"login.password":        "Password",
		"login.submit":          "Sign in",
		"login.failed":          "Sign in failed",
		"action.create":         "New Record",
		"action.edit":           "Edit",
		"action.delete":         "Delete",
		"action.cancel":         "Cancel",
		"action.save":           "Save",
		"action.confirm":        "Confirm",
		"action.retry":          "Retry",
		"action.refresh":        "Refresh",
		"action.export":         "Export / Import",
		"action.search":         "Search",
		"action.filters":        "Filters",
		"state.loading":         "Loading…",
		"state.empty":           "Nothing to show",
		"state.error":           "Something went wrong",
		"state.no_access":       "You do not have access to this",
		"dashboard.widgets":     "Your application",
		"dashboard.unavailable": "This card could not be read",
	},
	"es": {
		"nav.overview":          "Resumen",
		"nav.data_studio":       "Datos",
		"nav.system":            "Pulso del sistema",
		"nav.live":              "Inspector de red",
		"nav.sessions":          "Sesiones",
		"nav.health":            "Salud",
		"nav.operators":         "Operadores",
		"nav.rbac":              "Control de acceso",
		"nav.audit":             "Auditoría",
		"nav.main":              "Navegación principal",
		"nav.open":              "Abrir navegación",
		"nav.close":             "Cerrar navegación",
		"nav.expand":            "Desplegar barra lateral",
		"nav.collapse":          "Plegar barra lateral",
		"session.signed_in":     "Sesión iniciada",
		"session.active":        "Sesión de administración activa",
		"session.sign_out":      "Cerrar sesión",
		"theme.light":           "Cambiar al tema claro",
		"theme.dark":            "Cambiar al tema oscuro",
		"login.title":           "Iniciar sesión",
		"login.username":        "Usuario",
		"login.password":        "Contraseña",
		"login.submit":          "Entrar",
		"login.failed":          "No se pudo iniciar sesión",
		"action.create":         "Nuevo registro",
		"action.edit":           "Editar",
		"action.delete":         "Borrar",
		"action.cancel":         "Cancelar",
		"action.save":           "Guardar",
		"action.confirm":        "Confirmar",
		"action.retry":          "Reintentar",
		"action.refresh":        "Actualizar",
		"action.export":         "Exportar / Importar",
		"action.search":         "Buscar",
		"action.filters":        "Filtros",
		"state.loading":         "Cargando…",
		"state.empty":           "No hay nada que mostrar",
		"state.error":           "Algo ha fallado",
		"state.no_access":       "No tienes acceso a esto",
		"dashboard.widgets":     "Tu aplicación",
		"dashboard.unavailable": "No se ha podido leer esta tarjeta",
	},
}
