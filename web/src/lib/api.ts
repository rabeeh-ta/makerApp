// The Go server's JSON API. Forms post as application/x-www-form-urlencoded,
// which the handlers read with r.FormValue, so a <form>'s fields go as they are.

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

// Done is what a form post answers: the page to show next, or why it failed.
export type Done = { redirect?: string; error?: string }

let onSignedOut = () => {}

// whenSignedOut sets what happens when the session is gone (any 401).
export function whenSignedOut(fn: () => void) {
  onSignedOut = fn
}

async function call<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, { credentials: 'same-origin', ...init })
  let body: unknown = null
  if (res.headers.get('Content-Type')?.startsWith('application/json')) body = await res.json()
  // A 401 means the session is gone, except from the login form itself,
  // which answers 401 to a wrong password.
  if (res.status === 401 && path !== '/api/login') onSignedOut()
  if (!res.ok) {
    const msg = (body as Done | null)?.error
    throw new ApiError(res.status, msg || `Something went wrong (${res.status}). Try again.`)
  }
  return body as T
}

export function get<T>(path: string): Promise<T> {
  return call<T>(path)
}

export function post<T = Done>(path: string, data?: FormData | Record<string, string>): Promise<T> {
  const body = new URLSearchParams()
  if (data instanceof FormData) {
    for (const [k, v] of data) if (typeof v === 'string') body.append(k, v)
  } else if (data) {
    for (const [k, v] of Object.entries(data)) body.append(k, v)
  }
  return call<T>(path, { method: 'POST', body })
}
