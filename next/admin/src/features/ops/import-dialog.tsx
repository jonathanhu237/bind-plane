import { CircleAlert } from 'lucide-react'
import { useState } from 'react'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Textarea } from '@/components/ui/textarea'
import { callAssets, type Inventory } from '@/features/release/asset-editors'
import { errorText, serverText } from '@/features/release/messages'
import { ToneBadge, pick } from './ui'

type ImportKind = 'switches' | 'networks'
type ImportReport = {
  dry_run: boolean; committed: boolean; created: number; skipped: number; errors: number; new_groups: string[]
  rows: { line: number; status: 'create' | 'skip' | 'error'; label: string; message?: string }[]
}

const templates: Record<ImportKind, string> = {
  switches: 'name,management_ip,vendor,model,group,site,protocol,port,credential,profile,description\nSZ-CORE-01,10.18.127.10,huawei,S12700E-4,Core,SZ,ssh,,,,\n',
  networks: 'cidr,switch,vlan,description,validated\n10.88.117.0/24,10.18.127.10,117,Building 1,true\n',
}
// "No default" travels as this select value and is sent as ''.
const NONE = 'none'

export function ImportDialog({ open, zh, inventory, onClose, onImported }: {
  open: boolean; zh: boolean; inventory?: Inventory; onClose: () => void; onImported: () => void
}) {
  const [kind, setKind] = useState<ImportKind>('switches')
  const [csv, setCsv] = useState('')
  const [credential, setCredential] = useState('')
  const [profile, setProfile] = useState('')
  const [report, setReport] = useState<ImportReport | null>(null)
  const [previewed, setPreviewed] = useState('') // the exact request the clean preview was made for
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const request = JSON.stringify({ kind, csv, default_credential_id: credential, default_profile_id: profile })
  const canCommit = report !== null && report.dry_run && report.errors === 0 && report.created > 0 && previewed === request

  async function send(dryRun: boolean) {
    setBusy(true); setError('')
    try {
      const result = await callAssets<ImportReport>('import', { kind, csv, dry_run: dryRun, default_credential_id: credential, default_profile_id: profile })
      setReport(result)
      setPreviewed(dryRun ? request : '')
      if (result.committed) onImported()
    } catch (cause) { setError(errorText(cause, zh)) }
    finally { setBusy(false) }
  }
  function readFile(file: File | undefined) {
    if (!file) return
    void file.text().then((text) => { setCsv(text); setReport(null) })
  }
  const statusLabel = { create: pick(zh, '新建', 'Create'), skip: pick(zh, '跳过', 'Skip'), error: pick(zh, '错误', 'Error') }
  const defaultSelect = (id: string, label: string, value: string, onChange: (value: string) => void, items: { id: string; name: string }[]) =>
    <Field>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Select value={value || NONE} onValueChange={(next) => onChange(next === NONE ? '' : next)}>
        <SelectTrigger id={id}><SelectValue /></SelectTrigger>
        <SelectContent><SelectGroup>
          <SelectItem value={NONE}>{pick(zh, '不指定', 'None')}</SelectItem>
          {items.map((item) => <SelectItem key={item.id} value={item.id}>{item.name}</SelectItem>)}
        </SelectGroup></SelectContent>
      </Select>
    </Field>

  return <Dialog open={open} onOpenChange={(next) => { if (!next) onClose() }}>
    <DialogContent closeLabel={pick(zh, '关闭', 'Close')} className="flex max-h-[90dvh] flex-col gap-6 overflow-y-auto sm:max-w-3xl">
      <DialogHeader>
        <DialogTitle>{pick(zh, '批量导入', 'Bulk import')}</DialogTitle>
        <DialogDescription>{pick(zh, '先检查再导入：检查会完整模拟一次导入但不保存。任何一行有错误都不会写入；已存在的设备或网段会被跳过，不会被覆盖。', 'Preview first: it runs the whole import without saving. Nothing is written while any row has an error, and existing switches or networks are skipped, never overwritten.')}</DialogDescription>
      </DialogHeader>
      <FieldGroup>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="import-kind">{pick(zh, '导入内容', 'Import')}</FieldLabel>
            <Select value={kind} onValueChange={(value) => { setKind(value as ImportKind); setReport(null) }}>
              <SelectTrigger id="import-kind"><SelectValue /></SelectTrigger>
              <SelectContent><SelectGroup>
                <SelectItem value="switches">{pick(zh, '交换机', 'Switches')}</SelectItem><SelectItem value="networks">{pick(zh, '网段', 'Networks')}</SelectItem>
              </SelectGroup></SelectContent>
            </Select>
          </Field>
          <Field>
            <FieldLabel htmlFor="import-file">{pick(zh, 'CSV 文件', 'CSV file')}</FieldLabel>
            <Input id="import-file" type="file" accept=".csv,text/csv" onChange={(event) => readFile(event.target.files?.[0])} />
          </Field>
          {kind === 'switches' ? <>
            {defaultSelect('import-credential', pick(zh, '默认凭据（CSV 未填写时使用）', 'Default credential (when the CSV has none)'), credential, setCredential, inventory?.credentials ?? [])}
            {defaultSelect('import-profile', pick(zh, '默认命令模板（CSV 未填写时使用）', 'Default profile (when the CSV has none)'), profile, setProfile, inventory?.profiles ?? [])}
          </> : null}
        </div>
        <Field>
          <div className="flex items-center justify-between gap-2">
            <FieldLabel htmlFor="import-csv">{pick(zh, 'CSV 内容', 'CSV text')}</FieldLabel>
            <Button type="button" variant="link" size="sm" className="h-auto p-0" onClick={() => { setCsv(templates[kind]); setReport(null) }}>{pick(zh, '填入模板', 'Insert template')}</Button>
          </div>
          <Textarea id="import-csv" className="font-mono" rows={8} spellCheck={false} aria-describedby="import-csv-hint" value={csv} onChange={(event) => setCsv(event.target.value)} />
          <FieldDescription id="import-csv-hint">{pick(zh, '首行为表头。也接受其他系统导出的中文表头，如“设备名称、IP地址、厂商、分组、校区、协议”。', 'The first row is the header. Chinese headings from other inventory exports, such as 设备名称 and IP地址, are accepted too.')}</FieldDescription>
        </Field>
      </FieldGroup>
      {error ? <Alert variant="destructive"><CircleAlert aria-hidden="true" /><AlertDescription>{error}</AlertDescription></Alert> : null}
      {report ? <div className="flex flex-col gap-2" aria-live="polite">
        <p className="text-sm">{report.committed ? pick(zh, '已导入。', 'Imported.') : report.errors ? pick(zh, '有错误，未写入任何数据。', 'Nothing was written because some rows have errors.') : pick(zh, '检查通过，尚未写入。', 'Preview passed. Nothing has been written yet.')}
          <span className="ml-2 text-muted-foreground tabular-nums">{pick(zh, `新建 ${report.created} · 跳过 ${report.skipped} · 错误 ${report.errors}`, `${report.created} to create · ${report.skipped} skipped · ${report.errors} errors`)}</span></p>
        {report.new_groups.length ? <p className="text-xs text-muted-foreground">{pick(zh, '将创建分组：', 'Groups to create: ')}{report.new_groups.join(zh ? '、' : ', ')}</p> : null}
        <div className="max-h-64 overflow-auto rounded-md border"><Table>
          <TableHeader><TableRow><TableHead>{pick(zh, '行', 'Line')}</TableHead><TableHead>{pick(zh, '结果', 'Result')}</TableHead><TableHead>{pick(zh, '对象', 'Object')}</TableHead><TableHead>{pick(zh, '说明', 'Note')}</TableHead></TableRow></TableHeader>
          <TableBody>{report.rows.map((row) => <TableRow key={row.line}>
            <TableCell className="tabular-nums">{row.line}</TableCell>
            <TableCell><ToneBadge kind={row.status === 'error' ? 'bad' : row.status === 'create' ? 'good' : 'muted'}>{statusLabel[row.status]}</ToneBadge></TableCell>
            <TableCell>{row.label}</TableCell>
            <TableCell className="text-muted-foreground">{serverText(row.message, zh)}</TableCell>
          </TableRow>)}</TableBody>
        </Table></div>
      </div> : null}
      <DialogFooter>
        <Button type="button" variant="outline" disabled={busy || !csv.trim()} onClick={() => void send(true)}>{busy ? pick(zh, '处理中…', 'Working…') : pick(zh, '检查', 'Preview')}</Button>
        <Button type="button" disabled={busy || !canCommit} onClick={() => void send(false)}>{pick(zh, '导入', 'Import')}</Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
}
