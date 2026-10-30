import { useQuery } from '@tanstack/react-query'
import { CircleAlert, TriangleAlert } from 'lucide-react'
import { useId, useRef, useState } from 'react'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Field, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { cn } from '@/lib/utils'
import {
  assetOptionsKey, reasonLabel, reasons, releaseErrorText, request, statusLabel, stopText, targetIPProblem, type AssetOptions, type Preparation,
} from './jobs'

// "Resolve the switch automatically" travels as this select value and is sent as no selection.
const AUTO = 'auto'

type Props = {
  admin: boolean
  zh: boolean
  /** Offer admins the forced release of an address the query finds no record for. */
  allowForce?: boolean
  title?: string
  description?: string
  action?: React.ReactNode
  /** Receives the pre-release query job once the API has queued it. */
  onPrepared: (jobID: string) => void
}

/** The first step of a release: one IPv4 address and a reason, queued as a pre-release query. */
export function ReleaseQueryForm({ admin, zh, allowForce = false, title, description, action, onPrepared }: Props) {
  const id = useId()
  const [ip, setIP] = useState('')
  const [ipProblem, setIPProblem] = useState('')
  const ipInput = useRef<HTMLInputElement>(null)
  const [reason, setReason] = useState<string>('temporary_test')
  const [force, setForce] = useState(false)
  const [selectedSwitchID, setSelectedSwitchID] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [stopped, setStopped] = useState('')
  const forcing = allowForce && admin && force
  const options = useQuery({ queryKey: [assetOptionsKey], queryFn: () => request<AssetOptions>('/api/releases/admin/assets'), enabled: forcing, staleTime: 60_000 })
  const labels = zh ? {
    ip: 'IPv4 地址', reason: '释放原因', prepare: '查询绑定状态', preparing: '正在查询交换机…',
    force: '管理员强制释放（查询无记录）', forceSwitch: '执行强制释放的交换机', auto: '自动解析交换机',
  } : {
    ip: 'IPv4 address', reason: 'Reason', prepare: 'Query binding', preparing: 'Querying switch…',
    force: 'Admin force release (no record found)', forceSwitch: 'Switch to force the release on', auto: 'Resolve the switch automatically',
  }

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const problem = targetIPProblem(ip, zh)
    setIPProblem(problem)
    if (problem) { ipInput.current?.focus(); return }
    setBusy(true); setError(''); setStopped('')
    try {
      const data = await request<Preparation>('/api/releases/prepare', { target_ip: ip.trim(), reason, force: forcing, ...(forcing && selectedSwitchID ? { selected_switch_id: selectedSwitchID } : {}) })
      if (data.preparation_job_id) onPrepared(data.preparation_job_id)
      else setStopped(stopText(data.status, zh) || statusLabel(data.status, zh))
    } catch (cause) { setError(releaseErrorText(cause, zh)) }
    finally { setBusy(false) }
  }

  return <Card>
    {title ? <CardHeader className="flex-row flex-wrap items-start justify-between gap-x-4 gap-y-2">
      <div className="flex flex-col gap-1.5">
        <CardTitle className="text-lg">{title}</CardTitle>
        {description ? <CardDescription>{description}</CardDescription> : null}
      </div>
      {action}
    </CardHeader> : null}
    <CardContent className={cn(!title && 'pt-6')}>
      <form noValidate onSubmit={(event) => void submit(event)} className="flex flex-col gap-4">
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
          <Button type="submit" disabled={busy}>{busy ? labels.preparing : labels.prepare}</Button>
        </div>
        {allowForce && admin ? <div className="flex flex-col gap-3">
          <Field orientation="horizontal">
            <Checkbox id={`${id}-force`} checked={force} onCheckedChange={(checked) => setForce(checked === true)} />
            <FieldLabel htmlFor={`${id}-force`} className="font-normal">{labels.force}</FieldLabel>
          </Field>
          {force ? <Field className="max-w-sm">
            <FieldLabel htmlFor={`${id}-switch`}>{labels.forceSwitch}</FieldLabel>
            <Select value={selectedSwitchID || AUTO} onValueChange={(value) => setSelectedSwitchID(value === AUTO ? '' : value)}>
              <SelectTrigger id={`${id}-switch`}><SelectValue /></SelectTrigger>
              <SelectContent><SelectGroup>
                <SelectItem value={AUTO}>{labels.auto}</SelectItem>
                {options.data?.switches.filter((item) => item.is_enabled).map((item) => <SelectItem key={item.id} value={item.id}>{item.name}</SelectItem>)}
              </SelectGroup></SelectContent>
            </Select>
          </Field> : null}
        </div> : null}
        {error ? <Alert variant="destructive"><CircleAlert aria-hidden="true" /><AlertDescription>{error}</AlertDescription></Alert> : null}
        {stopped ? <Alert variant="warning"><TriangleAlert aria-hidden="true" /><AlertDescription>{stopped}</AlertDescription></Alert> : null}
      </form>
    </CardContent>
  </Card>
}
