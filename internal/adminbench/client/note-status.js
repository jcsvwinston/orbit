// The bench application's own client code (A11: EXT-06, EXT-07, UIX-12):
// what an application writes to draw one of its fields its own way.
//
// note-status draws a note's status as a badge for the statuses it knows,
// and throws on any other — the way a renderer written against the values
// an application expects meets one it did not. The panel declares this file
// (orbit.ClientCode), serves it under its prefix with the digest of these
// bytes, and loads it after its own bundle, so window.orbit is here.
(function () {
  'use strict'
  var orbit = window.orbit
  if (!orbit || orbit.version !== 1) {
    throw new Error('note-status.js: window.orbit (version 1) is not here')
  }
  var labels = { draft: 'Draft', published: 'Published' }
  orbit.registerFieldRenderer('note-status', function (value, context) {
    if (typeof value !== 'string' || !Object.prototype.hasOwnProperty.call(labels, value)) {
      throw new Error('no badge for the status ' + JSON.stringify(value))
    }
    var badge = document.createElement('span')
    badge.className = 'bench-note-status bench-note-status-' + value
    badge.setAttribute('data-bench-status', value)
    badge.setAttribute('data-bench-where', context.where)
    badge.textContent = labels[value]
    return badge
  })
})()
