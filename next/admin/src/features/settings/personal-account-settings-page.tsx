import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { KeyRound, Languages, Palette, Save, Send, ShieldCheck, Trash2, Upload, X } from 'lucide-react'
import { useEffect, useRef, useState, type ChangeEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel, FieldLegend, FieldSet } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Separator } from '@/components/ui/separator'
import { ApiProblemError, type ApiClient } from '@/shared/api/client'
import type { EmailChange, User } from '@/shared/api/contracts'
import { currentUserQueryKey } from '@/features/auth/queries'
import { changeAccountLocale } from '@/shared/i18n'
import { useOptionalTheme, type Theme } from '@/shared/theme'
import { notifyRequestError, notifySuccess } from '@/shared/feedback'
import { UserAvatar } from '@/features/auth/user-avatar'
import { passwordResetFormSchema, personalEmailChangeFormSchema, personalNameFormSchema } from '@/features/auth/schemas'

type Translate = (key: string, options?: Record<string, unknown>) => string

const avatarTypes = ['image/jpeg', 'image/png', 'image/webp'] as const
type AvatarType = (typeof avatarTypes)[number]
const avatarPreviewSide = 96

type AvatarSourceSize = { width: number; height: number }

type AvatarCropTransform = AvatarSourceSize & {
  scale: number
  renderedWidth: number
  renderedHeight: number
  maxPanX: number
  maxPanY: number
  panX: number
  panY: number
  sourceLeft: number
  sourceTop: number
  sourceSide: number
}

// The preview and the exported PNG use this same source-space transform. The
// base cover scale intentionally accounts for non-square images, so a wide or
// tall source can reach either edge even before the user increases zoom.
export function avatarCropTransform(source: AvatarSourceSize, zoom: number, pan: { x: number; y: number }, viewportSide = avatarPreviewSide): AvatarCropTransform {
  const width = Math.max(1, source.width)
  const height = Math.max(1, source.height)
  const side = Math.max(1, viewportSide)
  const boundedZoom = Math.max(1, Math.min(3, Number.isFinite(zoom) ? zoom : 1))
  const scale = side / Math.min(width, height) * boundedZoom
  const renderedWidth = width * scale
  const renderedHeight = height * scale
  const maxPanX = Math.max(0, (renderedWidth - side) / 2)
  const maxPanY = Math.max(0, (renderedHeight - side) / 2)
  const panX = Math.max(-maxPanX, Math.min(maxPanX, Number.isFinite(pan.x) ? pan.x : 0))
  const panY = Math.max(-maxPanY, Math.min(maxPanY, Number.isFinite(pan.y) ? pan.y : 0))
  const sourceSide = side / scale
  const sourceLeft = Math.max(0, Math.min(width - sourceSide, (width - sourceSide) / 2 - panX / scale))
  const sourceTop = Math.max(0, Math.min(height - sourceSide, (height - sourceSide) / 2 - panY / scale))
  return { width, height, scale, renderedWidth, renderedHeight, maxPanX, maxPanY, panX, panY, sourceLeft, sourceTop, sourceSide }
}

export function clampAvatarPan(value: number, axis: 'x' | 'y', source: AvatarSourceSize | null, zoom: number, viewportSide = avatarPreviewSide): number {
  if (!source) return 0
  const transform = avatarCropTransform(source, zoom, axis === 'x' ? { x: value, y: 0 } : { x: 0, y: value }, viewportSide)
  return axis === 'x' ? transform.panX : transform.panY
}

export function PersonalAccountSettingsPage({ api, userID }: { api: ApiClient; userID?: string }) {
  const { t } = useTranslation(['personalSettings', 'common', 'problems'])
  const translate = t as unknown as Translate
  const queryClient = useQueryClient()
  const theme = useOptionalTheme()
  const profileQuery = useQuery({
    queryKey: ['personal-profile', userID ?? 'current'],
    queryFn: ({ signal }) => api.getPersonalProfile ? api.getPersonalProfile(signal) : Promise.reject(new Error('missing getPersonalProfile')),
    retry: false,
  })
  const [user, setUser] = useState<User | undefined>()
  const [emailChange, setEmailChange] = useState<EmailChange | null>(null)
  const [name, setName] = useState('')
  const [locale, setLocale] = useState<'en' | 'zh-CN'>('en')
  const previousLocale = useRef<'en' | 'zh-CN'>('en')
  const ownerIDRef = useRef(userID)
  const [avatarError, setAvatarError] = useState('')
  const [nameError, setNameError] = useState(false)

  useEffect(() => {
    ownerIDRef.current = userID
  }, [userID])
  useEffect(() => {
    if (!profileQuery.data || (userID && profileQuery.data.user.id !== userID)) {
      if (userID && profileQuery.data?.user.id !== userID) {
        setUser(undefined)
        setEmailChange(null)
        setName('')
        setLocale('en')
        previousLocale.current = 'en'
        setAvatarError('')
        setNameError(false)
      }
      return
    }
    const preserveNameDraft = user?.id === profileQuery.data.user.id && name !== user.name
    setUser(profileQuery.data.user)
    setAvatarError('')
    setNameError(false)
    if (!preserveNameDraft) setName(profileQuery.data.user.name)
    if (profileQuery.data.user.locale === 'en' || profileQuery.data.user.locale === 'zh-CN') { setLocale(profileQuery.data.user.locale); previousLocale.current = profileQuery.data.user.locale } else { setLocale('en'); previousLocale.current = 'en' }
    setEmailChange(profileQuery.data.emailChange ?? null)
  }, [profileQuery.data, userID])

  const applyUser = (updated: User, patch: Partial<User>) => {
    if (ownerIDRef.current !== userID) return
    const cached = queryClient.getQueryData<User>(currentUserQueryKey)
    if ((userID && updated.id !== userID) || (cached && userID && cached.id !== userID)) return
    setUser((current) => current ? { ...current, ...patch } : updated)
    if (patch.name !== undefined) setName(patch.name)
    if (patch.locale === 'en' || patch.locale === 'zh-CN') setLocale(patch.locale)
    queryClient.setQueryData(currentUserQueryKey, cached ? { ...cached, ...patch } : updated)
    if (userID) queryClient.setQueryData<{ user: User; emailChange?: EmailChange | null }>(['personal-profile', userID], (profile) => profile ? { ...profile, user: { ...profile.user, ...patch } } : profile)
  }
  const saveName = useMutation({
    mutationFn: () => api.updatePersonalName ? api.updatePersonalName(name.trim()) : Promise.reject(new Error('missing updatePersonalName')),
    onSuccess: (updated) => { applyUser(updated, { name: updated.name }); notifySuccess(t('personalSettings:nameSaved')) },
    onError: (error) => notifyRequestError(error, t, { title: t('personalSettings:nameSaveFailed') }),
  })
  const saveLocale = useMutation({
    mutationFn: (next: 'en' | 'zh-CN') => api.updatePersonalLocale ? api.updatePersonalLocale(next) : Promise.reject(new Error('missing updatePersonalLocale')),
    onSuccess: async (updated, requestedLocale) => { if (ownerIDRef.current !== userID || (userID && updated.id !== userID)) return; const savedLocale = updated.locale === 'en' || updated.locale === 'zh-CN' ? updated.locale : requestedLocale; applyUser(updated, { locale: savedLocale }); previousLocale.current = savedLocale; await changeAccountLocale(savedLocale); notifySuccess(t('personalSettings:localeSaved')) },
    onError: (error) => { if (ownerIDRef.current !== userID) return; setLocale(previousLocale.current); void changeAccountLocale(previousLocale.current); notifyRequestError(error, t, { title: t('personalSettings:localeSaveFailed') }) },
  })
  const applyAvatar = (updated: User) => {
    if (ownerIDRef.current !== userID) return
    const cached = queryClient.getQueryData<User>(currentUserQueryKey)
    if ((userID && updated.id !== userID) || (cached && userID && cached.id !== userID)) return
    const avatar = { avatarUrl: updated.avatarUrl, hasAvatar: updated.hasAvatar, avatarVersion: updated.avatarVersion }
    applyUser(updated, avatar)
  }
  const saveAvatar = useMutation({
    mutationFn: (file: Blob) => api.savePersonalAvatar ? api.savePersonalAvatar(file) : Promise.reject(new Error('missing savePersonalAvatar')),
    onSuccess: (updated) => { applyAvatar(updated); setAvatarError(''); notifySuccess(t('personalSettings:avatarSaved')) },
    onError: (error) => { if (ownerIDRef.current !== userID) return; setAvatarError(avatarProblemCode(error) ? translate(`personalSettings:${avatarProblemCode(error)}`) : translate('personalSettings:avatarSaveFailed')); notifyRequestError(error, t) },
  })
  const removeAvatar = useMutation({
    mutationFn: () => api.removePersonalAvatar ? api.removePersonalAvatar() : Promise.reject(new Error('missing removePersonalAvatar')),
    onSuccess: (updated) => { applyAvatar(updated); notifySuccess(t('personalSettings:avatarRemoved')) },
    onError: (error) => notifyRequestError(error, t, { title: t('personalSettings:avatarSaveFailed') }),
  })
  if (profileQuery.isPending) return <p role="status">{t('common:loading')}</p>
  if (profileQuery.isError || !user) return <p role="status" className="text-sm text-muted-foreground">{t('personalSettings:loadFailed')}</p>

  return <div className="flex min-w-0 max-w-[68rem] flex-col gap-8" aria-labelledby="personal-settings-title">
    <h1 id="personal-settings-title" className="text-2xl font-semibold tracking-tight">{t('personalSettings:title')}</h1>
    <Separator />
    <section aria-labelledby="personal-profile-title" className="grid min-w-0 gap-5 lg:grid-cols-[12rem_minmax(0,1fr)] lg:gap-10">
      <h2 id="personal-profile-title" tabIndex={-1} className="scroll-mt-6 text-base font-semibold leading-6 focus:outline-none">{t('personalSettings:profile.title')}</h2>
      <div className="flex min-w-0 flex-col gap-6">
        <form className="flex flex-col gap-4" onSubmit={(event) => { event.preventDefault(); if (saveName.isPending) return; if (!personalNameFormSchema.safeParse({ name }).success) { setNameError(true); return }; setNameError(false); saveName.mutate() }} noValidate>
          <Field data-invalid={nameError}><FieldLabel htmlFor="personal-name">{t('personalSettings:profile.name')}</FieldLabel><Input id="personal-name" autoComplete="name" value={name} onChange={(event) => { setName(event.target.value); setNameError(false) }} aria-invalid={nameError} aria-describedby={nameError ? 'personal-name-error' : undefined} disabled={saveName.isPending} />{nameError ? <FieldError id="personal-name-error">{t('problems:fields.invalidName')}</FieldError> : null}</Field>
          <div className="flex justify-end"><Button type="submit" disabled={saveName.isPending || name.trim() === user.name}>{saveName.isPending ? t('common:saving') : t('common:save')}<Save aria-hidden="true" data-icon="inline-start" /></Button></div>
        </form>
        <Separator />
        <div className="flex flex-col gap-4"><FieldLabel>{t('personalSettings:profile.avatar')}</FieldLabel><FieldDescription>{t('personalSettings:profile.avatarDescription')}</FieldDescription><AvatarEditor key={user.id} user={user} error={avatarError} disabled={saveAvatar.isPending || removeAvatar.isPending} onError={setAvatarError} onSave={(file) => saveAvatar.mutateAsync(file).then(() => undefined)} onRemove={() => removeAvatar.mutate()} /></div>
        <Separator />
        <Field><FieldLabel htmlFor="personal-email">{t('personalSettings:profile.email')}</FieldLabel><Input id="personal-email" value={user.email} readOnly aria-readonly="true" /><FieldDescription>{t('personalSettings:profile.emailDescription')}</FieldDescription></Field>
      </div>
    </section>
    <Separator />
    <section aria-labelledby="personal-appearance-title" className="grid min-w-0 gap-5 lg:grid-cols-[12rem_minmax(0,1fr)] lg:gap-10">
      <h2 id="personal-appearance-title" tabIndex={-1} className="scroll-mt-6 text-base font-semibold leading-6 focus:outline-none">{t('personalSettings:appearance.title')}</h2>
      <div className="grid gap-6 sm:grid-cols-2">
        <Field><FieldLabel htmlFor="personal-locale"><Languages aria-hidden="true" data-icon="inline-start" />{t('personalSettings:appearance.language')}</FieldLabel><Select value={locale} onValueChange={(value) => { const next = value as 'en' | 'zh-CN'; previousLocale.current = locale; setLocale(next); void changeAccountLocale(next); saveLocale.mutate(next) }} disabled={saveLocale.isPending}><SelectTrigger id="personal-locale"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="zh-CN">{t('common:chinese')}</SelectItem><SelectItem value="en">{t('common:english')}</SelectItem></SelectContent></Select><FieldDescription>{t('personalSettings:appearance.languageDescription')}</FieldDescription></Field>
        <Field><FieldLabel><Palette aria-hidden="true" data-icon="inline-start" />{t('personalSettings:appearance.theme')}</FieldLabel><div className="grid gap-2" role="radiogroup" aria-label={t('personalSettings:appearance.theme')}>
          {(['light', 'dark', 'system'] as Theme[]).map((item) => <label key={item} className="flex cursor-pointer items-center gap-2 rounded-md border px-3 py-2 text-sm has-[:checked]:border-primary has-[:checked]:bg-primary/5"><input type="radio" name="personal-theme" value={item} checked={theme.theme === item} onChange={() => theme.setTheme(item)} />{t(`common:${item}`)}</label>)}
        </div><FieldDescription>{t('personalSettings:appearance.themeDescription')}</FieldDescription></Field>
      </div>
    </section>
    <Separator />
    <section aria-labelledby="personal-security-title" className="grid min-w-0 gap-5 lg:grid-cols-[12rem_minmax(0,1fr)] lg:gap-10">
      <h2 id="personal-security-title" tabIndex={-1} className="scroll-mt-6 text-base font-semibold leading-6 focus:outline-none">{t('personalSettings:security.title')}</h2>
      <div className="flex min-w-0 flex-col gap-8"><PasswordForm key={`password-${user.id}`} api={api} t={t as unknown as Translate} /><Separator /><EmailChangeForm key={`email-${user.id}`} api={api} t={t as unknown as Translate} emailChange={emailChange} setEmailChange={setEmailChange} /></div>
    </section>
  </div>
}

function AvatarEditor({ user, error, disabled, onError, onSave, onRemove }: { user: User; error: string; disabled: boolean; onError: (value: string) => void; onSave: (file: Blob) => Promise<void>; onRemove: () => void }) {
  const { t } = useTranslation(['personalSettings', 'common'])
  const translate = t as unknown as Translate
  const [source, setSource] = useState<string | null>(null)
  const [file, setFile] = useState<File | null>(null)
  const [sourceSize, setSourceSize] = useState<AvatarSourceSize | null>(null)
  const [zoom, setZoom] = useState(1)
  const [pan, setPan] = useState({ x: 0, y: 0 })
  const dragOrigin = useRef<{ x: number; y: number } | undefined>(undefined)
  const selectionSequence = useRef(0)
  const hasDraft = Boolean(source && file)
  useEffect(() => source ? () => URL.revokeObjectURL(source) : undefined, [source])
  const choose = async (event: ChangeEvent<HTMLInputElement>) => {
    const next = event.target.files?.[0]
    event.target.value = ''
    if (!next) return
    const selection = ++selectionSequence.current
    if (next.size > 5 * 1024 * 1024) { onError(translate('personalSettings:avatarTooLarge')); return }
    try {
      const format = await inspectAvatarFile(next)
      if (selection !== selectionSequence.current) return
      if (!format || format === 'unsupported' || (next.type && normalizeAvatarType(next.type) !== format)) { onError(translate(format === 'unsupported' ? 'personalSettings:avatarUnsupported' : 'personalSettings:avatarInvalid')); return }
    } catch {
      if (selection === selectionSequence.current) onError(translate('personalSettings:avatarInvalid'))
      return
    }
    if (selection !== selectionSequence.current) return
    const objectURL = URL.createObjectURL(next)
    setFile(next)
    setSource(objectURL)
    setSourceSize(null)
    setZoom(1)
    setPan({ x: 0, y: 0 })
    onError('')
  }
  const cancel = () => { setSource(null); setFile(null); setSourceSize(null); setZoom(1); setPan({ x: 0, y: 0 }); dragOrigin.current = undefined; onError('') }
  const save = async () => {
    if (!file || !source || !sourceSize) return
    let blob: Blob
    try {
      const image = await loadImage(source)
      const transform = avatarCropTransform({ width: image.naturalWidth, height: image.naturalHeight }, zoom, pan)
      const canvas = document.createElement('canvas'); canvas.width = 256; canvas.height = 256
      const context = canvas.getContext('2d'); if (!context) throw new Error('canvas unavailable')
      context.drawImage(image, transform.sourceLeft, transform.sourceTop, transform.sourceSide, transform.sourceSide, 0, 0, 256, 256)
      const converted = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, 'image/png'))
      if (!converted) throw new Error('image conversion failed')
      blob = converted
    } catch {
      onError(translate('personalSettings:avatarInvalid'))
      return
    }
    try {
      await onSave(blob)
      cancel()
    } catch {
      // Keep the non-sensitive crop draft so the user can retry after a
      // server or transport failure. The parent mutation supplies feedback.
    }
  }
  const preview = sourceSize ? avatarCropTransform(sourceSize, zoom, pan) : undefined
  return <div className="flex flex-wrap items-start gap-5">
    <div data-testid="avatar-crop-preview" data-source-ready={sourceSize ? 'true' : 'false'} className="relative flex size-24 shrink-0 touch-none items-center justify-center overflow-hidden rounded-xl border bg-muted" onPointerDown={(event) => { if (!hasDraft || !preview) return; event.currentTarget.setPointerCapture(event.pointerId); dragOrigin.current = { x: event.clientX - preview.panX, y: event.clientY - preview.panY } }} onPointerMove={(event) => { if (!dragOrigin.current || !sourceSize) return; setPan({ x: clampAvatarPan(event.clientX - dragOrigin.current.x, 'x', sourceSize, zoom), y: clampAvatarPan(event.clientY - dragOrigin.current.y, 'y', sourceSize, zoom) }) }} onPointerUp={() => { dragOrigin.current = undefined }} onPointerCancel={() => { dragOrigin.current = undefined }}>{hasDraft ? <img src={source ?? undefined} alt={translate('personalSettings:profile.avatarPreview')} className={preview ? 'absolute max-w-none cursor-grab active:cursor-grabbing' : 'size-full object-cover'} draggable={false} onLoad={(event) => { const image = event.currentTarget; if (image.naturalWidth > 0 && image.naturalHeight > 0) setSourceSize({ width: image.naturalWidth, height: image.naturalHeight }) }} onError={() => onError(translate('personalSettings:avatarInvalid'))} style={preview ? { width: `${preview.renderedWidth}px`, height: `${preview.renderedHeight}px`, left: `calc(50% + ${preview.panX}px)`, top: `calc(50% + ${preview.panY}px)`, transform: 'translate(-50%, -50%)' } : undefined} /> : <UserAvatar user={user} className="size-full rounded-xl text-xl" />}</div>
    <div className="flex min-w-[15rem] flex-1 flex-col gap-3"><div className="flex flex-wrap gap-2"><label className="inline-flex"><input type="file" className="sr-only" accept="image/jpeg,image/png,image/webp" onChange={(event) => { void choose(event) }} disabled={disabled} /><Button type="button" variant="outline" asChild disabled={disabled}><span><Upload aria-hidden="true" data-icon="inline-start" />{translate('personalSettings:chooseAvatar')}</span></Button></label>{user.hasAvatar && !hasDraft ? <Button type="button" variant="ghost" onClick={onRemove} disabled={disabled}><Trash2 aria-hidden="true" data-icon="inline-start" />{translate('personalSettings:removeAvatar')}</Button> : null}{hasDraft ? <><Button type="button" onClick={() => void save()} disabled={disabled || !sourceSize}><Save aria-hidden="true" data-icon="inline-start" />{translate('personalSettings:saveAvatar')}</Button><Button type="button" variant="ghost" onClick={cancel} disabled={disabled}><X aria-hidden="true" data-icon="inline-start" />{translate('common:cancel')}</Button></> : null}</div>{hasDraft ? <Field><FieldLabel htmlFor="avatar-zoom">{translate('personalSettings:avatarZoom')}</FieldLabel><input id="avatar-zoom" type="range" min="1" max="3" step="0.05" value={zoom} onChange={(event) => { const next = Number(event.target.value); setZoom(next); setPan((current) => ({ x: clampAvatarPan(current.x, 'x', sourceSize, next), y: clampAvatarPan(current.y, 'y', sourceSize, next) })) }} disabled={disabled || !sourceSize} /></Field> : null}{error ? <FieldError>{error}</FieldError> : <FieldDescription>{translate('personalSettings:profile.avatarDescription')}</FieldDescription>}</div>
  </div>
}

function PasswordForm({ api, t }: { api: ApiClient; t: Translate }) {
  const [currentPassword, setCurrentPassword] = useState(''); const [newPassword, setNewPassword] = useState(''); const [confirmPassword, setConfirmPassword] = useState('')
  const [validation, setValidation] = useState<{ field: 'newPassword' | 'confirmPassword'; code: 'invalid_password' | 'password_mismatch' }>()
  const mutation = useMutation({ mutationFn: () => api.changePersonalPassword ? api.changePersonalPassword({ currentPassword, newPassword, confirmPassword }) : Promise.reject(new Error('missing changePersonalPassword')), onSuccess: () => { setCurrentPassword(''); setNewPassword(''); setConfirmPassword(''); setValidation(undefined); notifySuccess(t('personalSettings:passwordSaved')); window.location.assign('/login') }, onError: (error) => notifyRequestError(error, t, { title: t('personalSettings:passwordSaveFailed') }) })
  const submit = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const parsed = passwordResetFormSchema.safeParse({ password: newPassword, passwordConfirmation: confirmPassword })
    if (!parsed.success) {
      const issue = parsed.error.issues.find((item) => item.path[0] === 'passwordConfirmation') ?? parsed.error.issues[0]
      setValidation({ field: issue?.path[0] === 'passwordConfirmation' ? 'confirmPassword' : 'newPassword', code: issue?.message === 'password_mismatch' ? 'password_mismatch' : 'invalid_password' })
      return
    }
    setValidation(undefined)
    mutation.mutate()
  }
  return <form className="flex flex-col gap-5" onSubmit={submit} noValidate><FieldSet><FieldLegend className="flex items-center gap-2"><KeyRound aria-hidden="true" />{t('personalSettings:security.passwordTitle')}</FieldLegend><FieldDescription>{t('personalSettings:security.passwordDescription')}</FieldDescription><FieldGroup><Field><FieldLabel htmlFor="current-password">{t('personalSettings:security.currentPassword')}</FieldLabel><Input id="current-password" type="password" autoComplete="current-password" value={currentPassword} onChange={(event) => setCurrentPassword(event.target.value)} disabled={mutation.isPending} /></Field><div className="grid gap-4 sm:grid-cols-2"><Field data-invalid={validation?.field === 'newPassword'}><FieldLabel htmlFor="new-password">{t('personalSettings:security.newPassword')}</FieldLabel><Input id="new-password" type="password" autoComplete="new-password" value={newPassword} onChange={(event) => { setNewPassword(event.target.value); if (validation?.field === 'newPassword') setValidation(undefined) }} aria-invalid={validation?.field === 'newPassword'} aria-describedby={validation?.field === 'newPassword' ? 'personal-password-error' : undefined} disabled={mutation.isPending} />{validation?.field === 'newPassword' ? <FieldError id="personal-password-error">{t('problems:fields.invalidPassword')}</FieldError> : null}</Field><Field data-invalid={validation?.field === 'confirmPassword'}><FieldLabel htmlFor="confirm-password">{t('personalSettings:security.confirmPassword')}</FieldLabel><Input id="confirm-password" type="password" autoComplete="new-password" value={confirmPassword} onChange={(event) => { setConfirmPassword(event.target.value); if (validation?.field === 'confirmPassword') setValidation(undefined) }} aria-invalid={validation?.field === 'confirmPassword'} aria-describedby={validation?.field === 'confirmPassword' ? 'personal-password-error' : undefined} disabled={mutation.isPending} />{validation?.field === 'confirmPassword' ? <FieldError id="personal-password-error">{t('problems:fields.passwordMismatch')}</FieldError> : null}</Field></div></FieldGroup></FieldSet><div className="flex justify-end"><Button type="submit" disabled={mutation.isPending || !currentPassword || !newPassword || !confirmPassword}>{mutation.isPending ? t('common:saving') : t('personalSettings:security.savePassword')}<ShieldCheck aria-hidden="true" data-icon="inline-start" /></Button></div></form>
}

function EmailChangeForm({ api, t, emailChange, setEmailChange }: { api: ApiClient; t: Translate; emailChange: EmailChange | null; setEmailChange: (value: EmailChange | null) => void }) {
  const [currentPassword, setCurrentPassword] = useState(''); const [newEmail, setNewEmail] = useState(''); const [code, setCode] = useState('')
  const [emailError, setEmailError] = useState(false)
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (!emailChange) return
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [emailChange?.resendAvailableAt])
  const request = useMutation({ mutationFn: () => api.requestPersonalEmailChange ? api.requestPersonalEmailChange({ currentPassword, newEmail }) : Promise.reject(new Error('missing requestPersonalEmailChange')), onSuccess: (value) => { setEmailChange(value); setCurrentPassword(''); notifySuccess(t('personalSettings:security.codeSent')) }, onError: (error) => notifyRequestError(error, t, { title: t('personalSettings:security.emailChangeFailed') }) })
  const resend = useMutation({ mutationFn: () => api.resendPersonalEmailChange ? api.resendPersonalEmailChange() : Promise.reject(new Error('missing resendPersonalEmailChange')), onSuccess: (value) => { setEmailChange(value); setCode(''); notifySuccess(t('personalSettings:security.codeSent')) }, onError: (error) => notifyRequestError(error, t) })
  const verify = useMutation({ mutationFn: () => api.verifyPersonalEmailChange ? api.verifyPersonalEmailChange({ requestId: emailChange?.id ?? '', code }) : Promise.reject(new Error('missing verifyPersonalEmailChange')), onSuccess: () => { notifySuccess(t('personalSettings:security.emailChanged')); window.location.assign('/login') }, onError: async (error) => { if (api.getPersonalEmailChange) { try { setEmailChange(await api.getPersonalEmailChange()) } catch { /* Keep the current request visible when a refresh also fails. */ } } notifyRequestError(error, t, { title: t('personalSettings:security.verifyFailed') }) } })
  const resendDisabled = emailChange ? emailChange.attemptsRemaining <= 0 || new Date(emailChange.resendAvailableAt).getTime() > now : true
  const submitRequest = (event: React.FormEvent<HTMLFormElement>) => { event.preventDefault(); if (request.isPending || !currentPassword || !newEmail) return; if (!personalEmailChangeFormSchema.safeParse({ email: newEmail }).success) { setEmailError(true); return }; setEmailError(false); request.mutate() }
  const submitVerification = (event: React.FormEvent<HTMLFormElement>) => { event.preventDefault(); if (!verify.isPending && code.length === 6) verify.mutate() }
  return <div className="flex flex-col gap-5"><FieldSet><FieldLegend className="flex items-center gap-2"><Languages aria-hidden="true" />{t('personalSettings:security.emailTitle')}</FieldLegend><FieldDescription>{t('personalSettings:security.emailDescription')}</FieldDescription>{!emailChange ? <form className="flex flex-col gap-4" onSubmit={submitRequest} noValidate><FieldGroup><Field><FieldLabel htmlFor="email-current-password">{t('personalSettings:security.currentPassword')}</FieldLabel><Input id="email-current-password" type="password" autoComplete="current-password" value={currentPassword} onChange={(event) => setCurrentPassword(event.target.value)} disabled={request.isPending} /></Field><Field data-invalid={emailError}><FieldLabel htmlFor="new-email">{t('personalSettings:security.newEmail')}</FieldLabel><Input id="new-email" type="email" autoComplete="email" value={newEmail} onChange={(event) => { setNewEmail(event.target.value); setEmailError(false) }} aria-invalid={emailError} aria-describedby={emailError ? 'personal-email-error' : undefined} disabled={request.isPending} />{emailError ? <FieldError id="personal-email-error">{t('problems:fields.invalidEmail')}</FieldError> : null}</Field></FieldGroup><div className="flex justify-end"><Button type="submit" disabled={request.isPending || !currentPassword || !newEmail}>{request.isPending ? t('common:saving') : t('personalSettings:security.sendCode')}<MailIcon aria-hidden="true" /></Button></div></form> : <form className="flex flex-col gap-4 rounded-lg border bg-muted/30 p-4" onSubmit={submitVerification} noValidate><p className="text-sm">{t('personalSettings:security.codeSentTo', { email: emailChange.newEmail })}</p><p className="text-xs text-muted-foreground">{t('personalSettings:security.codeValidity')}</p><Field><FieldLabel htmlFor="email-code">{t('personalSettings:security.verificationCode')}</FieldLabel><Input id="email-code" inputMode="numeric" autoComplete="one-time-code" maxLength={6} pattern="[0-9]{6}" value={code} onChange={(event) => setCode(event.target.value.replace(/\D/g, '').slice(0, 6))} disabled={verify.isPending} /></Field><p className="text-xs text-muted-foreground">{t('personalSettings:security.attemptsRemaining', { count: emailChange.attemptsRemaining })}</p>{resendDisabled ? <p className="text-xs text-muted-foreground">{t('personalSettings:security.resendIn', { seconds: Math.ceil(Math.max(0, new Date(emailChange.resendAvailableAt).getTime() - now) / 1000) })}</p> : null}<div className="flex flex-wrap gap-2"><Button type="submit" disabled={verify.isPending || code.length !== 6}>{verify.isPending ? t('common:saving') : t('personalSettings:security.verify')}</Button><Button type="button" variant="outline" onClick={() => resend.mutate()} disabled={resend.isPending || resendDisabled}>{t('personalSettings:security.resend')}</Button><Button type="button" variant="ghost" onClick={() => { setEmailChange(null); setCode(''); setNewEmail(''); setCurrentPassword('') }} disabled={verify.isPending}><X aria-hidden="true" data-icon="inline-start" />{t('common:cancel')}</Button></div></form>}</FieldSet></div>
}

function MailIcon(props: { 'aria-hidden': 'true' }) { return <Send {...props} /> }

function normalizeAvatarType(value: string): string {
  const type = value.toLowerCase().split(';', 1)[0]?.trim()
  return type === 'image/jpg' ? 'image/jpeg' : type
}

async function inspectAvatarFile(file: File): Promise<AvatarType | 'unsupported' | null> {
  const bytes = new Uint8Array(await file.arrayBuffer())
  const lowerText = new TextDecoder().decode(bytes.subarray(0, 4096)).toLowerCase()
  if (/<svg(?:\\s|>)/.test(lowerText)) return 'unsupported'
  if (bytes.length >= 8 && bytes[0] === 0x89 && bytes[1] === 0x50 && bytes[2] === 0x4e && bytes[3] === 0x47 && bytes[4] === 0x0d && bytes[5] === 0x0a && bytes[6] === 0x1a && bytes[7] === 0x0a) {
    if (containsBytes(bytes, [0x61, 0x63, 0x54, 0x4c])) return 'unsupported'
    return 'image/png'
  }
  if (bytes.length >= 3 && bytes[0] === 0xff && bytes[1] === 0xd8 && bytes[2] === 0xff) return 'image/jpeg'
  if (bytes.length >= 12 && bytes[0] === 0x52 && bytes[1] === 0x49 && bytes[2] === 0x46 && bytes[3] === 0x46 && bytes[8] === 0x57 && bytes[9] === 0x45 && bytes[10] === 0x42 && bytes[11] === 0x50) {
    if (containsBytes(bytes, [0x41, 0x4e, 0x49, 0x4d]) || containsBytes(bytes, [0x41, 0x4e, 0x4d, 0x46])) return 'unsupported'
    return 'image/webp'
  }
  return null
}

function containsBytes(haystack: Uint8Array, needle: number[]): boolean {
  for (let index = 0; index + needle.length <= haystack.length; index += 1) {
    if (needle.every((value, offset) => haystack[index + offset] === value)) return true
  }
  return false
}

function loadImage(source: string): Promise<HTMLImageElement> { return new Promise((resolve, reject) => { const image = new Image(); image.onload = () => resolve(image); image.onerror = reject; image.src = source }) }

function avatarProblemCode(error: unknown): string | null {
  if (!(error instanceof ApiProblemError)) return null
  if (error.problem.type === '/problems/content-too-large') return 'avatarTooLarge'
  if (error.problem.type === '/problems/unsupported-media-type') return 'avatarUnsupported'
  const code = error.problem.errors?.[0]?.code
  if (code === 'unsupported_type') return 'avatarUnsupported'
  if (code === 'invalid_avatar') return 'avatarInvalid'
  if (code === 'content-too-large') return 'avatarTooLarge'
  return null
}
