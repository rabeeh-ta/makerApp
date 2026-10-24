// A small history-API router. Every navigation bumps a counter, so a page
// reloads its data even when a form sends it back to the same URL.
import { useEffect, useRef, useState, useSyncExternalStore, type AnchorHTMLAttributes, type FormEvent } from 'react'
import { get, post, type Done } from './api'
import { toast } from './toast'

let version = 0
const listeners = new Set<() => void>()

function emit() {
  version++
  listeners.forEach((l) => l())
}
window.addEventListener('popstate', emit)

export function navigate(to: string, opts: { replace?: boolean } = {}) {
  const same = to === location.pathname + location.search
  if (opts.replace || same) history.replaceState(null, '', to)
  else {
    history.pushState(null, '', to)
    window.scrollTo(0, 0)
  }
  emit()
}

function subscribe(l: () => void) {
  listeners.add(l)
  return () => {
    listeners.delete(l)
  }
}

export function useLocation() {
  const v = useSyncExternalStore(subscribe, () => version)
  return { path: location.pathname, query: new URLSearchParams(location.search), version: v }
}

export function Link({ to, onClick, ...rest }: { to: string } & AnchorHTMLAttributes<HTMLAnchorElement>) {
  return (
    <a
      href={to}
      {...rest}
      onClick={(e) => {
        onClick?.(e)
        if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return
        e.preventDefault()
        navigate(to)
      }}
    />
  )
}

// useData loads path, again after every navigation, and on reload().
// stamp changes with each load, to reset forms filled from the data.
export function useData<T>(path: string | null) {
  const { version } = useLocation()
  const [tick, setTick] = useState(0)
  const [state, setState] = useState<{ path: string | null; data?: T; error: string; stamp: number }>({
    path: null,
    error: '',
    stamp: 0,
  })
  useEffect(() => {
    if (!path) return
    let live = true
    get<T>(path).then(
      (data) => live && setState((s) => ({ path, data, error: '', stamp: s.stamp + 1 })),
      (e: Error) => live && setState((s) => ({ ...s, path, error: e.message })),
    )
    return () => {
      live = false
    }
  }, [path, version, tick])
  const current = state.path === path
  return {
    data: current ? state.data : undefined,
    error: current ? state.error : '',
    stamp: state.stamp,
    reload: () => setTick((t) => t + 1),
  }
}

export function useInterval(fn: () => void, ms: number | null) {
  const ref = useRef(fn)
  useEffect(() => {
    ref.current = fn
  })
  useEffect(() => {
    if (ms == null) return
    const id = setInterval(() => ref.current(), ms)
    return () => clearInterval(id)
  }, [ms])
}

// useForm posts a form's fields to the API: a redirect navigates, an error
// shows on the page.
export function useForm() {
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  function submit(action: string, opts: { confirm?: string; done?: (r: Done) => void } = {}) {
    return async (e: FormEvent<HTMLFormElement>) => {
      e.preventDefault()
      const form = e.currentTarget
      if (opts.confirm && !window.confirm(opts.confirm)) return
      setBusy(true)
      setError('')
      try {
        const r = await post(action, new FormData(form))
        opts.done?.(r)
        if (r.redirect) navigate(r.redirect)
      } catch (err) {
        setError((err as Error).message)
      } finally {
        setBusy(false)
      }
    }
  }
  return { error, busy, submit, setError }
}

// PostButton posts one action (install, move, …) and follows its redirect.
export function PostButton({
  action,
  data,
  confirm,
  className = 'btn small',
  title,
  disabled,
  children,
}: {
  action: string
  data?: Record<string, string>
  confirm?: string
  className?: string
  title?: string
  disabled?: boolean
  children: React.ReactNode
}) {
  const [busy, setBusy] = useState(false)
  async function click() {
    if (confirm && !window.confirm(confirm)) return
    setBusy(true)
    try {
      const r = await post(action, data)
      if (r.redirect) navigate(r.redirect)
    } catch (err) {
      toast((err as Error).message)
    } finally {
      setBusy(false)
    }
  }
  return (
    <button type="button" className={className} title={title} disabled={disabled || busy} onClick={click}>
      {children}
    </button>
  )
}
