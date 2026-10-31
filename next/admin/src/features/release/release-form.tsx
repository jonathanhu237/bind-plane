import { useQuery } from '@tanstack/react-query'
import { CircleAlert, TriangleAlert } from 'lucide-react'
import { useId, useRef, useState } from 'react'
import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Field, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import {
  assetOptionsKey, reasonLabel, reasons, releaseErrorText, request, statusLabel, stopText, targetIPProblem, type AssetOptions, type Started,
} from './jobs'

// "Resolve the switch automatically" travels as this select value and is sent as no selection.
const AUTO = 'auto'

type Props = {
  /** Admins may also force a release, which deletes even when the switch has no record of the address. */
  admin: boolean
  zh: boolean
  /** Receives the release job once the API has queued it. */
  onStarted: (jobID: string) => void
}

/** Starts a release in one step: the worker queries the binding, deletes it only when it is static, and queries again. */
export function ReleaseForm({ admin, zh, onStarted }: Props) {
  const id = useId()
  const [ip, setIP] = useState('')
  const [ipProblem, setIPProblem] = useState('')
  const ipInput = useRef<HTMLInputElement>(null)
  const [reason, setReason] = useState<string>('temporary_test')
  const [force, setForce] = useState(false)
  const [confirmingForce, setConfirmingForce] = useState(false)
  const [selectedSwitchID, setSelectedSwitchID] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [stopped, setStopped] = useState('')
  const forcing = admin && force
  const options = useQuery({ queryKey: [assetOptionsKey], queryFn: () => request<AssetOptions>('/api/releases/admin/assets'), enabled: forcing, staleTime: 60_000 })
  const labels = zh ? {
    ip: 'IPv4 地址', reason: '解绑原因', release: '解绑', releasing: '正在提交…',
    force: '管理员强制解绑：交换机上查不到记录也执行删除', forceSwitch: '执行强制解绑的交换机', auto: '自动解析交换机',
    forceTitle: (address: string) => `强制解绑 ${address}？`,
    forceImpact: '交换机上查不到这个 IP 的记录时，仍会执行删除命令，并在解绑记录和审计日志中标为强制。只有确定这条绑定存在、只是查询不到时才这样做。',
    cancel: '取消', confirmForce: '强制解绑',
  } : {
    ip: 'IPv4 address', reason: 'Reason', release: 'Release', releasing: 'Submitting…',
    force: 'Admin force release: delete even when the switch has no record', forceSwitch: 'Switch to force the release on', auto: 'Resolve the switch automatically',
    forceTitle: (address: string) => `Force the release of ${address}?`,
    forceImpact: 'The release command runs even when the switch has no record of this IP, and the release is marked as forced in the history and the audit log. Do this only when you know the binding exists but the query cannot see it.',
    cancel: 'Cancel', confirmForce: 'Force release',
  }

  function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const problem = targetIPProblem(ip, zh)
    setIPProblem(problem)
    if (problem) { ipInput.current?.focus(); return }
    // A forced release is confirmed first; a normal one starts right away.
    if (forcing) setConfirmingForce(true)
    else void start()
  }

  async function start() {
    setBusy(true); setError(''); setStopped('')
    try {
      const data = await request<Started>('/api/releases', { target_ip: ip.trim(), reason, force: forcing, ...(forcing && selectedSwitchID ? { selected_switch_id: selectedSwitchID } : {}) })
      if (data.job_id) {
        setIP(''); setForce(false); setSelectedSwitchID('')
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
        {admin ? <div className="flex flex-col gap-3">
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
      <AlertDialog open={confirmingForce} onOpenChange={setConfirmingForce}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{labels.forceTitle(ip.trim())}</AlertDialogTitle>
            <AlertDialogDescription>{labels.forceImpact}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{labels.cancel}</AlertDialogCancel>
            <AlertDialogAction onClick={() => void start()}>{labels.confirmForce}</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </CardContent>
  </Card>
}
