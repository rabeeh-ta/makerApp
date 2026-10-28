import { ErrorNotice, Loading, Notice } from '../components/common'
import { useTitle } from '../lib/format'
import { Link, useData, useForm, useLocation } from '../lib/router'

type AccountData = {
  Email: string
  Username: string
  CreatedAt: number
  Devices: number
  Online: number
  Apps: number
  Sources: number
  AIApps: number
}

// Account is the page behind the avatar: who's signed in, what the account
// holds, and the password.
export function Account() {
  useTitle('Account')
  const { query } = useLocation()
  const { data, error, stamp } = useData<AccountData>('/api/account')
  if (!data) return <Loading error={error} />
  const since = new Date(data.CreatedAt * 1000).toLocaleDateString(undefined, { year: 'numeric', month: 'long', day: 'numeric' })
  const stats = [
    { to: '/devices', label: 'Devices', n: data.Devices, sub: data.Devices ? `${data.Online} online` : 'None paired yet' },
    { to: '/apps', label: 'Apps', n: data.Apps, sub: 'Made by you' },
    { to: '/sources', label: 'Data sources', n: data.Sources, sub: 'Feeding your screens' },
    { to: '/connect', label: 'AI apps', n: data.AIApps, sub: 'Connected over MCP' },
  ]
  return (
    <>
      <div className="page-head">
        <h1>Account</h1>
      </div>
      {query.has('changed') && <Notice>Password changed. Any other browser signed in to this account was signed out.</Notice>}

      <div className="grid two">
        <section className="card">
          <h3>Profile</h3>
          <dl className="kv">
            <dt>Email</dt>
            <dd>{data.Email}</dd>
            <dt>Public name</dt>
            <dd>{data.Username ? `@${data.Username}` : <span className="muted">Not chosen yet. You pick it when you first publish an app.</span>}</dd>
            <dt>Member since</dt>
            <dd>{since}</dd>
          </dl>
        </section>
        <Password key={stamp} email={data.Email} />
      </div>

      <h3 className="section-title">In your account</h3>
      <div className="grid stats">
        {stats.map((s) => (
          <Link key={s.to} to={s.to} className="card">
            <span className="muted small">{s.label}</span>
            <span className="stat-num">{s.n}</span>
            <span className="muted small">{s.sub}</span>
          </Link>
        ))}
      </div>
    </>
  )
}

function Password({ email }: { email: string }) {
  const { error, busy, submit } = useForm()
  return (
    <section className="card">
      <h3>Change password</h3>
      <ErrorNotice error={error} />
      <form onSubmit={submit('/api/account/password')} className="stack">
        {/* Tells password managers which account the new password is for. */}
        <input type="email" name="email" value={email} autoComplete="username" readOnly hidden />
        <label>
          Current password
          <input type="password" name="current" autoComplete="current-password" required />
        </label>
        <label>
          New password <span className="muted small">at least 8 characters</span>
          <input type="password" name="password" autoComplete="new-password" minLength={8} required />
        </label>
        <label>
          New password again
          <input type="password" name="confirm" autoComplete="new-password" minLength={8} required />
        </label>
        <button className="btn" disabled={busy}>
          Change password
        </button>
        <p className="muted small">This signs you out of every other browser. Connected AI apps stay connected.</p>
      </form>
    </section>
  )
}
