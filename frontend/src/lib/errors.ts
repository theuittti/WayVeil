// Classifies the Human() texts of internal/gui/errors.go (keep the prefixes in sync).

const reconnect = ['The connection to the server was lost', 'Not connected to the server']

// The SSH session is gone: the only way on is to connect again.
export function needsReconnect(msg: string): boolean {
  return reconnect.some((p) => msg.startsWith(p))
}

// The saved host key does not match: shown as a red (danger) warning.
export function isHostKeyChanged(msg: string): boolean {
  return msg.startsWith("The server's identity changed")
}
