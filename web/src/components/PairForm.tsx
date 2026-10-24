import { ErrorNotice } from './common'
import { useForm } from '../lib/router'

// PairForm adds a board that's showing a 6-digit code to the account.
export function PairForm({ autoFocus }: { autoFocus?: boolean }) {
  const { error, busy, submit } = useForm()
  return (
    <>
      <ErrorNotice error={error} />
      <form onSubmit={submit('/api/pair')} className="pair-form">
        <input
          name="code"
          aria-label="Code on the board's screen"
          inputMode="numeric"
          pattern="[0-9 ]{6,7}"
          placeholder="123 456"
          className="code-input"
          autoComplete="one-time-code"
          required
          autoFocus={autoFocus}
        />
        <button className="btn" disabled={busy}>Pair device</button>
      </form>
    </>
  )
}

// PairCard offers pairing next to the flashing steps: a board that already
// has the firmware shows a code instead of needing a flash.
export function PairCard({ wide }: { wide?: boolean }) {
  return (
    <section className={wide ? 'card pair-card wide' : 'card pair-card'}>
      <div>
        <h3>Board showing a 6-digit code?</h3>
        <p className="muted small">It already has the firmware. Type the code from its screen to add it to your account.</p>
      </div>
      <PairForm />
    </section>
  )
}
