import { useLoaderData } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { CircleAlert, Copy, Pencil, Power, PowerOff } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { isAdmin } from '@/features/ops/ui'
import { cn } from '@/lib/utils'
import { notifyRequestError, notifySuccess } from '@/shared/feedback'
import {
  AssetEditDialog, AssetRequestError, DisableAssetDialog, callAssets, editorLabels, isOn, statusBody, type AssetKind, type EditorLabels, type EditTarget, type Inventory,
} from './asset-editors'
import { errorText } from './messages'

/**
 * What the asset settings pages share: the inventory, the edit and disable dialogs, and creating,
 * enabling and disabling an asset. Render `dialogs` once on the page.
 */
export function useAssetPage() {
  const { user } = useLoaderData({ from: '/_authenticated' })
  const { i18n, t } = useTranslation()
  const zh = i18n.language.startsWith('zh')
  const admin = isAdmin(user)
  const inventory = useQuery({ queryKey: ['release-assets', user.id], queryFn: () => callAssets<Inventory>('assets'), enabled: admin })
  const [busy, setBusy] = useState(false)
  const [editing, setEditing] = useState<EditTarget | null>(null)
  const [disabling, setDisabling] = useState<EditTarget | null>(null)
  const labels = editorLabels(zh)

  async function refresh() { await inventory.refetch() }
  // Errors stay in the open editor; a stale version also reloads the list behind it.
  async function saveEdit(path: string, body: Record<string, unknown>) {
    try { await callAssets(path, body, 'PATCH') }
    catch (error) { if (error instanceof AssetRequestError && error.status === 409) await refresh(); throw error }
    setEditing(null); notifySuccess(labels.saved); await refresh()
  }
  async function setStatus(target: EditTarget, on: boolean) {
    setBusy(true)
    try { await callAssets(`${target.kind}/${target.item.id}`, statusBody(target, on), 'PATCH'); notifySuccess(labels.statusChanged(target, on)) }
    catch (error) { notifyRequestError(error, t, { description: errorText(error, zh) }) }
    finally { await refresh(); setBusy(false) }
  }
  // Enabling is immediate; disabling explains its effect on resolution and queued jobs first.
  function toggle(target: EditTarget) { if (isOn(target)) setDisabling(target); else void setStatus(target, true) }
  /** Creates an asset and reloads the inventory; a failure is thrown for the dialog to show. */
  async function create(kind: AssetKind, body: unknown) {
    await callAssets(kind, body); notifySuccess(labels.saved); await refresh()
  }

  const dialogs = <>
    <AssetEditDialog target={editing} inventory={inventory.data} zh={zh} onClose={() => setEditing(null)} onSave={saveEdit} />
    <DisableAssetDialog target={disabling} inventory={inventory.data} zh={zh} onCancel={() => setDisabling(null)} onConfirm={(target) => void setStatus(target, false)} />
  </>
  return { zh, admin, inventory, data: inventory.data, labels, busy, edit: setEditing, toggle, create, dialogs }
}

/** The actions at the end of an asset's row. */
export function AssetActions({ title, on, busy, labels, onEdit, onCopy, onToggle }: {
  title: string; on: boolean; busy: boolean; labels: EditorLabels; onEdit: () => void; onCopy?: () => void; onToggle: () => void
}) {
  return <div className="flex justify-end gap-1">
    <Button type="button" variant="ghost" size="sm" disabled={busy} onClick={onEdit} aria-label={`${labels.edit} ${title}`}><Pencil aria-hidden="true" />{labels.edit}</Button>
    {onCopy ? <Button type="button" variant="ghost" size="sm" disabled={busy} onClick={onCopy} aria-label={`${labels.copy} ${title}`}><Copy aria-hidden="true" />{labels.copy}</Button> : null}
    <Button type="button" variant="ghost" size="sm" disabled={busy} onClick={onToggle} aria-label={`${on ? labels.disable : labels.enable} ${title}`}>
      {on ? <PowerOff aria-hidden="true" /> : <Power aria-hidden="true" />}{on ? labels.disable : labels.enable}
    </Button>
  </div>
}

/** A dialog with one form that creates an asset; a failed save keeps it open with the reason. */
export function CreateAssetDialog({ open, zh, title, description, wide = false, error, saving, onClose, onSubmit, children }: {
  open: boolean; zh: boolean; title: string; description?: string; wide?: boolean; error: string; saving: boolean
  onClose: () => void; onSubmit: () => void; children: React.ReactNode
}) {
  return <Dialog open={open} onOpenChange={(next) => { if (!next) onClose() }}>
    <DialogContent closeLabel={zh ? '关闭' : 'Close'} className={cn('max-h-[90dvh] overflow-y-auto', wide ? 'sm:max-w-3xl' : 'sm:max-w-lg')}>
      <form className="flex flex-col gap-6" noValidate onSubmit={(event) => { event.preventDefault(); onSubmit() }}>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          {description ? <DialogDescription>{description}</DialogDescription> : null}
        </DialogHeader>
        {children}
        {error ? <Alert variant="destructive"><CircleAlert aria-hidden="true" /><AlertDescription>{error}</AlertDescription></Alert> : null}
        <DialogFooter>
          <DialogClose asChild><Button type="button" variant="outline">{zh ? '取消' : 'Cancel'}</Button></DialogClose>
          <Button type="submit" disabled={saving}>{saving ? (zh ? '正在保存…' : 'Saving…') : (zh ? '保存' : 'Save')}</Button>
        </DialogFooter>
      </form>
    </DialogContent>
  </Dialog>
}
