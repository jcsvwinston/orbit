module github.com/jcsvwinston/orbit/ui

go 1.26.6

// ui is the one frontend project of Orbit (ADR-015): the in-process panel
// and the fleet plane, two entries over shared sources, built into one dist
// this module embeds. The root module (the panel) and server/ (the fleet)
// require it BY TAG and serve their subtree; it depends on nothing, like
// the datasource contract (ADR-006, ADR-012).
