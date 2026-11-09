import { Check, CircleAlert, Pencil, Plus, Trash2, X } from 'lucide-react'
import { useState } from 'react'
import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { callAssets, type GroupAsset, type Inventory } from '@/features/release/asset-editors'
import { errorText } from '@/features/release/messages'
import { notifySuccess } from '@/shared/feedback'
import { Empty, pick } from './ui'

/**
 * Adds, renames and deletes switch groups. Deleting a group leaves its switches ungrouped; the API
 * refuses while a schedule or compliance rule still targets it.
 */
export function GroupManager({ zh, inventory, onChanged }: { zh: boolean; inventory?: Inventory; onChanged: () => Promise<unknown> | void }) {
  const [name, setName] = useState('')
  const [editing, setEditing] = useState<{ id: string; name: string; description: string } | null>(null)
  const [deleting, setDeleting] = useState<GroupAsset | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const groups = inventory?.groups ?? []
  const members = (id: string) => inventory?.switches.filter((item) => item.group_id === id).length ?? 0
  const switches = (count: number) => pick(zh, `${count} 台交换机`, `${count} switch${count === 1 ? '' : 'es'}`)

  async function act(work: () => Promise<unknown>, done: string) {
    setBusy(true); setError('')
    try { await work(); notifySuccess(done); await onChanged(); return true }
    catch (cause) { setError(errorText(cause, zh)); return false }
    finally { setBusy(false) }
  }
  async function add(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!name.trim()) return
    if (await act(() => callAssets('groups', { name: name.trim() }), pick(zh, '分组已添加。', 'Group added.'))) setName('')
  }
  async function rename(group: GroupAsset) {
    if (!editing?.name.trim()) return
    if (await act(() => callAssets(`groups/${group.id}`, { version: group.version, name: editing.name, description: editing.description }, 'PATCH'), pick(zh, '已保存。', 'Saved.'))) setEditing(null)
  }
  async function remove(group: GroupAsset) {
    setDeleting(null)
    await act(() => callAssets(`groups/${group.id}`, undefined, 'DELETE'), pick(zh, `分组「${group.name}」已删除。`, `Group “${group.name}” deleted.`))
  }

  return <div className="flex flex-col gap-4">
    <form className="flex gap-2" noValidate onSubmit={(event) => void add(event)}>
      <Input aria-label={pick(zh, '新分组名称', 'New group name')} placeholder={pick(zh, '新分组名称', 'New group name')} value={name} onChange={(event) => setName(event.target.value)} autoComplete="off" />
      <Button type="submit" variant="outline" disabled={busy || !name.trim()}><Plus aria-hidden="true" data-icon="inline-start" />{pick(zh, '添加', 'Add')}</Button>
    </form>
    {error ? <Alert variant="destructive"><CircleAlert aria-hidden="true" /><AlertDescription>{error}</AlertDescription></Alert> : null}
    {groups.length ? <ul className="divide-y rounded-xl border">{groups.map((group) => editing?.id === group.id
      ? <li key={group.id} className="flex flex-col gap-2 px-3 py-2">
        <Input aria-label={pick(zh, '分组名称', 'Group name')} value={editing.name} onChange={(event) => setEditing({ ...editing, name: event.target.value })} autoComplete="off" autoFocus />
        <Input aria-label={pick(zh, '描述（可选）', 'Description (optional)')} placeholder={pick(zh, '描述（可选）', 'Description (optional)')} value={editing.description} onChange={(event) => setEditing({ ...editing, description: event.target.value })} autoComplete="off" />
        <div className="flex justify-end gap-1">
          <Button type="button" variant="ghost" size="sm" onClick={() => setEditing(null)}><X aria-hidden="true" data-icon="inline-start" />{pick(zh, '取消', 'Cancel')}</Button>
          <Button type="button" size="sm" disabled={busy || !editing.name.trim()} onClick={() => void rename(group)}><Check aria-hidden="true" data-icon="inline-start" />{pick(zh, '保存', 'Save')}</Button>
        </div>
      </li>
      : <li key={group.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2">
        <div className="min-w-0 flex-[1_1_10rem]">
          <p className="break-words text-sm font-medium">{group.name}</p>
          <p className="break-words text-xs text-muted-foreground">{[switches(members(group.id)), group.description].filter(Boolean).join(' · ')}</p>
        </div>
        <div className="ml-auto flex shrink-0 gap-1">
          <Button type="button" variant="ghost" size="sm" disabled={busy} aria-label={`${pick(zh, '编辑', 'Edit')} ${group.name}`} onClick={() => setEditing({ id: group.id, name: group.name, description: group.description ?? '' })}><Pencil aria-hidden="true" />{pick(zh, '编辑', 'Edit')}</Button>
          <Button type="button" variant="ghost" size="sm" disabled={busy} aria-label={`${pick(zh, '删除', 'Delete')} ${group.name}`} onClick={() => setDeleting(group)}><Trash2 aria-hidden="true" />{pick(zh, '删除', 'Delete')}</Button>
        </div>
      </li>)}</ul>
      : <Empty>{pick(zh, '还没有分组。导入交换机时也会自动创建分组。', 'No groups yet. Importing switches creates their groups too.')}</Empty>}
    <AlertDialog open={deleting !== null} onOpenChange={(open) => { if (!open) setDeleting(null) }}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{deleting ? pick(zh, `删除分组「${deleting.name}」？`, `Delete group “${deleting.name}”?`) : ''}</AlertDialogTitle>
          <AlertDialogDescription>{deleting ? pick(zh,
            `${switches(members(deleting.id))}会变为未分组，设备本身不受影响。限定在这个分组的计划任务或合规规则需要先修改。`,
            `${switches(members(deleting.id))} become ungrouped; the switches themselves are untouched. Schedules or compliance rules that target the group must be changed first.`) : ''}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{pick(zh, '取消', 'Cancel')}</AlertDialogCancel>
          <AlertDialogAction onClick={() => { if (deleting) void remove(deleting) }}>{pick(zh, '删除', 'Delete')}</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  </div>
}
