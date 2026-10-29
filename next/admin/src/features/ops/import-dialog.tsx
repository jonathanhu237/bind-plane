import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Label } from '@/components/ui/label'
import { assetErrorText, callAssets, selectClass, type Inventory } from '@/features/release/asset-editors'
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
    } catch (cause) { setError(assetErrorText(cause, zh)) }
    finally { setBusy(false) }
  }
  function readFile(file: File | undefined) {
    if (!file) return
    void file.text().then((text) => { setCsv(text); setReport(null) })
  }
  const statusLabel = { create: pick(zh, '新建', 'Create'), skip: pick(zh, '跳过', 'Skip'), error: pick(zh, '错误', 'Error') }

  return <Dialog open={open} onOpenChange={(next) => { if (!next) onClose() }}>
    <DialogContent closeLabel={pick(zh, '关闭', 'Close')} className="max-h-[90dvh] overflow-y-auto sm:max-w-3xl">
      <DialogHeader>
        <DialogTitle>{pick(zh, '批量导入', 'Bulk import')}</DialogTitle>
        <DialogDescription>{pick(zh, '先检查再导入：检查会完整模拟一次导入但不保存。任何一行有错误都不会写入；已存在的设备或网段会被跳过，不会被覆盖。', 'Preview first: it runs the whole import without saving. Nothing is written while any row has an error, and existing switches or networks are skipped, never overwritten.')}</DialogDescription>
      </DialogHeader>
      <div className="grid gap-4 sm:grid-cols-2">
        <div className="space-y-2"><Label htmlFor="import-kind">{pick(zh, '导入内容', 'Import')}</Label>
          <select id="import-kind" className={selectClass} value={kind} onChange={(event) => { setKind(event.target.value as ImportKind); setReport(null) }}>
            <option value="switches">{pick(zh, '交换机', 'Switches')}</option><option value="networks">{pick(zh, '网段', 'Networks')}</option>
          </select></div>
        <div className="space-y-2"><Label htmlFor="import-file">{pick(zh, 'CSV 文件', 'CSV file')}</Label>
          <input id="import-file" type="file" accept=".csv,text/csv" className="block w-full text-sm file:mr-3 file:rounded-md file:border file:bg-background file:px-3 file:py-1.5 file:text-sm" onChange={(event) => readFile(event.target.files?.[0])} /></div>
        {kind === 'switches' ? <>
          <div className="space-y-2"><Label htmlFor="import-credential">{pick(zh, '默认凭据（CSV 未填写时使用）', 'Default credential (when the CSV has none)')}</Label>
            <select id="import-credential" className={selectClass} value={credential} onChange={(event) => setCredential(event.target.value)}>
              <option value="">—</option>{inventory?.credentials.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}
            </select></div>
          <div className="space-y-2"><Label htmlFor="import-profile">{pick(zh, '默认命令模板（CSV 未填写时使用）', 'Default profile (when the CSV has none)')}</Label>
            <select id="import-profile" className={selectClass} value={profile} onChange={(event) => setProfile(event.target.value)}>
              <option value="">—</option>{inventory?.profiles.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}
            </select></div>
        </> : null}
      </div>
      <div className="space-y-2">
        <div className="flex items-center justify-between gap-2"><Label htmlFor="import-csv">{pick(zh, 'CSV 内容', 'CSV text')}</Label>
          <Button type="button" variant="link" size="sm" className="h-auto p-0" onClick={() => { setCsv(templates[kind]); setReport(null) }}>{pick(zh, '填入模板', 'Insert template')}</Button></div>
        <textarea id="import-csv" rows={8} spellCheck={false} value={csv} onChange={(event) => setCsv(event.target.value)} className="w-full rounded-md border bg-background px-3 py-2 font-mono text-xs focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" />
        <p className="text-xs text-muted-foreground">{pick(zh, '首行为表头。也接受其他系统导出的中文表头，如“设备名称、IP地址、厂商、分组、校区、协议”。', 'The first row is the header. Chinese headings from other inventory exports, such as 设备名称 and IP地址, are accepted too.')}</p>
      </div>
      {error ? <p role="alert" className="rounded-md border border-destructive/40 px-3 py-2 text-sm text-destructive">{error}</p> : null}
      {report ? <div className="space-y-2" aria-live="polite">
        <p className="text-sm">{report.committed ? pick(zh, '已导入。', 'Imported.') : report.errors ? pick(zh, '有错误，未写入任何数据。', 'Nothing was written because some rows have errors.') : pick(zh, '检查通过，尚未写入。', 'Preview passed. Nothing has been written yet.')}
          <span className="ml-2 text-muted-foreground tabular-nums">{pick(zh, `新建 ${report.created} · 跳过 ${report.skipped} · 错误 ${report.errors}`, `${report.created} to create · ${report.skipped} skipped · ${report.errors} errors`)}</span></p>
        {report.new_groups.length ? <p className="text-xs text-muted-foreground">{pick(zh, '将创建分组：', 'Groups to create: ')}{report.new_groups.join(', ')}</p> : null}
        <div className="max-h-64 overflow-auto rounded-md border"><table className="w-full text-left text-xs"><thead className="sticky top-0 bg-muted"><tr><th className="px-2 py-1.5">{pick(zh, '行', 'Line')}</th><th className="px-2 py-1.5">{pick(zh, '结果', 'Result')}</th><th className="px-2 py-1.5">{pick(zh, '对象', 'Object')}</th><th className="px-2 py-1.5">{pick(zh, '说明', 'Note')}</th></tr></thead>
          <tbody>{report.rows.map((row) => <tr key={row.line} className="border-t"><td className="px-2 py-1 tabular-nums">{row.line}</td><td className="px-2 py-1"><ToneBadge kind={row.status === 'error' ? 'bad' : row.status === 'create' ? 'good' : 'muted'}>{statusLabel[row.status]}</ToneBadge></td><td className="px-2 py-1">{row.label}</td><td className="px-2 py-1 text-muted-foreground">{row.message}</td></tr>)}</tbody></table></div>
      </div> : null}
      <DialogFooter>
        <Button type="button" variant="outline" disabled={busy || !csv.trim()} onClick={() => void send(true)}>{busy ? pick(zh, '处理中…', 'Working…') : pick(zh, '检查', 'Preview')}</Button>
        <Button type="button" disabled={busy || !canCommit} onClick={() => void send(false)}>{pick(zh, '导入', 'Import')}</Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
}
