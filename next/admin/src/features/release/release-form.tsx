import { CircleAlert, TriangleAlert } from 'lucide-react'
import { useId, useRef, useState } from 'react'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Field, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { reasonLabel, reasons, releaseErrorText, request, statusLabel, stopText, targetIPProblem, type Started } from './jobs'

type Props = {
  zh: boolean
  /** Receives the release job once the API has queued it. */
  onStarted: (jobID: string) => void
}

/** Starts a release in one step: the worker queries the binding, deletes it only when it is static, and queries again. */
export function ReleaseForm({ zh, onStarted }: Props) {
  const id = useId()
  const [ip, setIP] = useState('')
  const [ipProblem, setIPProblem] = useState('')
  const ipInput = useRef<HTMLInputElement>(null)
  const [reason, setReason] = useState<string>('temporary_test')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [stopped, setStopped] = useState('')
  const labels = zh ? {
    ip: 'IPv4 地址', reason: '解绑原因', release: '解绑', releasing: '正在提交…',
  } : {
    ip: 'IPv4 address', reason: 'Reason', release: 'Release', releasing: 'Submitting…',
  }

  function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const problem = targetIPProblem(ip, zh)
    setIPProblem(problem)
    if (problem) { ipInput.current?.focus(); return }
    void start()
  }

  async function start() {
    setBusy(true); setError(''); setStopped('')
    try {
      const data = await request<Started>('/api/releases', { target_ip: ip.trim(), reason })
      if (data.job_id) {
        setIP('')
        onStarted(data.job_id)
      } else setStopped(stopText(data.status, zh) || statusLabel(data.status, zh))
    } catch (cause) { setError(releaseErrorText(cause, zh)) }
    finally { setBusy(false) }
  }

  return <Card>
    <CardContent className="pt-6">
      <form noValidate onSubmit={submit} className="flex flex-col gap-4">
        <div className="grid gap-4 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] sm:items-end">
          <Field data-invalid={Boolean(ipProblem)}>
            <FieldLabel htmlFor={`${id}-ip`}>{labels.ip}</FieldLabel>
            <Input id={`${id}-ip`} ref={ipInput} inputMode="decimal" autoComplete="off" placeholder="10.0.0.42" className="font-mono" value={ip}
              onChange={(event) => { setIP(event.target.value); setIPProblem('') }} aria-invalid={ipProblem ? true : undefined} aria-describedby={ipProblem ? `${id}-ip-problem` : undefined} />
          </Field>
          {/* Under the address on phones; on wider screens a row of its own, so the bottom-aligned row keeps its line. */}
          {ipProblem ? <FieldError id={`${id}-ip-problem`} className="sm:order-last sm:col-span-3 sm:-mt-2">{ipProblem}</FieldError> : null}
          <Field>
            <FieldLabel htmlFor={`${id}-reason`}>{labels.reason}</FieldLabel>
            <Select value={reason} onValueChange={setReason}>
              <SelectTrigger id={`${id}-reason`}><SelectValue /></SelectTrigger>
              <SelectContent><SelectGroup>{reasons.map((value) => <SelectItem key={value} value={value}>{reasonLabel(value, zh)}</SelectItem>)}</SelectGroup></SelectContent>
            </Select>
          </Field>
          <Button type="submit" disabled={busy}>{busy ? labels.releasing : labels.release}</Button>
        </div>
        {error ? <Alert variant="destructive"><CircleAlert aria-hidden="true" /><AlertDescription>{error}</AlertDescription></Alert> : null}
        {stopped ? <Alert variant="warning"><TriangleAlert aria-hidden="true" /><AlertDescription>{stopped}</AlertDescription></Alert> : null}
      </form>
    </CardContent>
  </Card>
}
