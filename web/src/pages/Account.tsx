import { ErrorNotice, Loading, Notice } from '../components/common'
import { plural, useTitle } from '../lib/format'
import { Link, useData, useForm, useLocation } from '../lib/router'
import { setTheme, themes, useTheme } from '../lib/theme'

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

// Account is the profile page behind the avatar: who you are and what your
// account holds, then your details, appearance, password and connected AI apps.
export function Account() {
  useTitle('Account')
  const { query } = useLocation()
  const { data, error, stamp } = useData<AccountData>('/api/account')
  if (!data) return <Loading error={error} />
  const since = new Date(data.CreatedAt * 1000).toLocaleDateString(undefined, { year: 'numeric', month: 'long', day: 'numeric' })
  const name = data.Username || data.Email.split('@')[0]
  const stats = [
    { to: '/devices', label: 'Devices', n: data.Devices, sub: data.Devices ? `${data.Online} online` : 'None paired yet' },
    { to: '/apps', label: 'Apps', n: data.Apps, sub: 'Made by you' },
    { to: '/sources', label: 'Data sources', n: data.Sources, sub: 'Feeding your screens' },
    { to: '/connect', label: 'AI apps', n: data.AIApps, sub: 'Connected over MCP' },
  ]
  return (
    <>
      {query.has('changed') && <Notice>Password changed. Any other browser signed in to this account was signed out.</Notice>}
      <section className="card profile-head">
        <span className="profile-avatar" aria-hidden="true">
          {name.slice(0, 1)}
        </span>
        <div>
          <h1>{data.Username ? `@${data.Username}` : name}</h1>
          <p className="muted">{data.Email}</p>
          <p className="muted small">
            Member since {since}
            {data.Username && ' · Publishes in the App Store'}
          </p>
        </div>
      </section>

      <div className="grid stats profile-stats">
        {stats.map((s) => (
          <Link key={s.to} to={s.to} className="card">
            <span className="muted small">{s.label}</span>
            <span className="stat-num">{s.n}</span>
            <span className="muted small">{s.sub}</span>
          </Link>
        ))}
      </div>

      <div className="grid two">
        <div className="stack">
          <section className="card">
            <h3>Account</h3>
            <dl className="kv">
              <dt>Email</dt>
              <dd>{data.Email}</dd>
              <dt>Public name</dt>
              <dd>{data.Username ? `@${data.Username}` : <span className="muted">Not chosen yet. You pick it when you first publish an app.</span>}</dd>
              <dt>Member since</dt>
              <dd>{since}</dd>
            </dl>
          </section>
          <Appearance />
        </div>
        <div className="stack">
          <Password key={stamp} email={data.Email} />
          <section className="card stack tight">
            <h3>Connected AI apps</h3>
            <p className="muted small">
              {data.AIApps ? `${plural(data.AIApps, 'AI app')} can build apps for you over MCP.` : 'No AI app is connected.'}{' '}
              <Link to="/connect">{data.AIApps ? 'Manage them' : 'Connect one'}</Link>
            </p>
          </section>
        </div>
      </div>
    </>
  )
}

function Appearance() {
  const theme = useTheme()
  return (
    <section className="card stack tight">
      <h3>Appearance</h3>
      <fieldset className="seg chips">
        <legend className="sr-only">Theme</legend>
        {themes.map((t) => (
          <label key={t.value}>
            <input type="radio" name="theme" checked={theme === t.value} onChange={() => setTheme(t.value)} />
            {t.label}
          </label>
        ))}
      </fieldset>
      <p className="muted small">System follows your computer's light or dark setting. This browser remembers your choice.</p>
    </section>
  )
}

function Password({ email }: { email: string }) {
  const { error, busy, submit } = useForm()
  return (
    <section className="card">
      <h3>Security</h3>
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
