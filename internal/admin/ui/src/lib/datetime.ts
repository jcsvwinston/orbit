// <input type="datetime-local"> speaks "YYYY-MM-DDTHH:mm" in the browser's
// local zone and carries no offset; the API speaks RFC 3339. Slicing the
// ISO string to 16 chars (the old approach) showed UTC wall-clock time as if
// it were local and then saved it back shifted by the zone offset.

const pad = (n: number) => String(n).padStart(2, '0')

// isoToLocalInput converts an RFC 3339 timestamp to the local wall-clock
// value the input expects. Unparseable values come back unchanged so the
// operator can see (and fix) what is stored.
export function isoToLocalInput(value: string): string {
  if (!value) return ''
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return value
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

// localInputToISO converts the input's local value to RFC 3339 with the
// local zone offset ("2026-09-03T10:30:00+02:00"), so the instant round-trips
// and the stored value still reads as the operator typed it.
export function localInputToISO(value: string): string {
  if (!value) return ''
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return value
  const offsetMin = -d.getTimezoneOffset()
  const sign = offsetMin >= 0 ? '+' : '-'
  const abs = Math.abs(offsetMin)
  const offset = `${sign}${pad(Math.floor(abs / 60))}:${pad(abs % 60)}`
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}${offset}`
}
