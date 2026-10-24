import { useEffect, useState, type FormEvent } from 'react'
import { ErrorNotice } from '../components/common'
import { post } from '../lib/api'
import { useTitle } from '../lib/format'
import { Link, navigate, useLocation } from '../lib/router'
import type { Me } from '../lib/types'

export function Auth({ mode, me, refreshMe }: { mode: 'login' | 'signup'; me: Me; refreshMe: () => Promise<void> }) {
  const signup = mode === 'signup'
  useTitle(signup ? 'Create account' : 'Log in')
  const next = useLocation().query.get('next') ?? ''
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    if (signup && !me.AllowSignup) navigate('/login', { replace: true })
  }, [signup, me.AllowSignup])

  async function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const form = e.currentTarget
    setBusy(true)
    setError('')
    try {
      const r = await post(`/api/${mode}`, new FormData(form))
      // Know who's signed in before the next page asks.
      await refreshMe()
      navigate(r.redirect ?? '/devices')
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="narrow card">
      <h1>{signup ? 'Create your account' : 'Welcome back'}</h1>
      <ErrorNotice error={error} />
      <form onSubmit={submit} className="stack">
        <input type="hidden" name="next" value={next} />
        <label>
          Email
          <input type="email" name="email" required autoFocus autoComplete="email" />
        </label>
        <label>
          Password
          <input type="password" name="password" required minLength={signup ? 8 : 1} autoComplete={signup ? 'new-password' : 'current-password'} />
        </label>
        <button className="btn" disabled={busy}>{signup ? 'Create account' : 'Log in'}</button>
      </form>
      <p className="muted small">
        {signup ? (
          <>
            Already have an account? <Link to="/login">Log in</Link>
          </>
        ) : (
          me.AllowSignup && (
            <>
              New here? <Link to={next ? `/signup?next=${encodeURIComponent(next)}` : '/signup'}>Create an account</Link>
            </>
          )
        )}
      </p>
    </section>
  )
}
