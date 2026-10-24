import { useState } from 'react'
import { ErrorNotice, Loading } from '../components/common'
import { post } from '../lib/api'
import { useTitle } from '../lib/format'
import { useData, useLocation } from '../lib/router'

type Info = { Client: string; RedirectHost: string; Email: string }

// OAuthAuthorize is where an AI app (Claude, Codex, …) sends the user to
// approve its access. Either answer sends the browser back to the app.
export function OAuthAuthorize() {
  useTitle('Connect an AI app')
  const query = useLocation().query.toString()
  const { data, error } = useData<Info>('/api/oauth/authorize?' + query)
  const [busy, setBusy] = useState(false)
  const [failed, setFailed] = useState('')

  async function decide(decision: 'allow' | 'deny') {
    setBusy(true)
    try {
      const fields = Object.fromEntries(new URLSearchParams(query))
      const r = await post('/api/oauth/authorize', { ...fields, decision })
      if (r.redirect) window.location.assign(r.redirect) // back to the AI app, outside makerApp
    } catch (err) {
      setFailed((err as Error).message)
      setBusy(false)
    }
  }

  if (!data) return <Loading error={error} />
  return (
    <section className="narrow card consent">
      <h1>Connect {data.Client}?</h1>
      <p className="muted">
        {data.Client} wants to build apps in your makerApp account, <b>{data.Email}</b>.
      </p>
      <ErrorNotice error={failed} />
      <h3>It can</h3>
      <ul>
        <li>See your boards, apps and data sources</li>
        <li>Create and change apps, screens and data sources</li>
        <li>Install and uninstall your apps on your boards</li>
      </ul>
      <h3>It can't</h3>
      <ul>
        <li>See or add your API keys</li>
        <li>Delete anything or publish apps</li>
        <li>Install apps other people published</li>
      </ul>
      <p className="muted small">
        Afterwards you go back to <span className="mono">{data.RedirectHost}</span>. You can disconnect it any time under AI Builder.
      </p>
      <div className="row consent-actions">
        <button type="button" className="btn ghost" disabled={busy} onClick={() => decide('deny')}>
          Cancel
        </button>
        <button type="button" className="btn" disabled={busy} onClick={() => decide('allow')}>
          Allow
        </button>
      </div>
    </section>
  )
}
