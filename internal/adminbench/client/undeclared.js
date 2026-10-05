// In the bench application's files and never declared: the panel serves
// what the application declared, not whatever its file system holds
// (EXT-06 asks for this file and expects a 404).
window.__benchUndeclared = true
