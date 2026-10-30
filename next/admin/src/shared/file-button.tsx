import { Upload } from 'lucide-react'
import { forwardRef } from 'react'
import { Button } from '@/components/ui/button'

type Props = {
  id: string
  accept: string
  label: string
  /** The chosen file's name, or what to say while there is none. */
  status: string
  disabled?: boolean
  onFile: (file: File | undefined) => void
}

/** A file input shown as a button beside the chosen name, so the browser's own "Choose file" wording never appears. */
export const FileButton = forwardRef<HTMLInputElement, Props>(function FileButton({ id, accept, label, status, disabled, onFile }, ref) {
  return <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2">
    <label className="inline-flex rounded-md has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-ring">
      <input ref={ref} id={id} type="file" className="sr-only" accept={accept} disabled={disabled} onChange={(event) => onFile(event.target.files?.[0])} />
      <Button type="button" variant="outline" asChild disabled={disabled}><span><Upload aria-hidden="true" data-icon="inline-start" />{label}</span></Button>
    </label>
    <span className="min-w-0 truncate text-sm text-muted-foreground">{status}</span>
  </div>
})
