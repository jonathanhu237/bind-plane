import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { PreferencesButtons } from './preferences-menu'
import { IdentityMark } from '@/features/identity/system-identity'

export function AuthPage({
  title,
  children,
}: {
  title: string
  children: React.ReactNode
}) {
  return (
    <main className="flex min-h-dvh items-center justify-center overflow-hidden bg-background px-4 py-10 sm:px-6">
      <Card className="w-full max-w-md">
        <CardHeader className="flex-row items-start justify-between gap-4">
          <div className="flex min-w-0 flex-1 flex-col gap-4">
            <IdentityMark />
            <h1 className="text-2xl font-semibold leading-tight tracking-tight sm:text-3xl">{title}</h1>
          </div>
          <PreferencesButtons className="shrink-0" />
        </CardHeader>
        <CardContent>{children}</CardContent>
      </Card>
    </main>
  )
}

export function AuthAlert({ title, description }: { title?: string; description: string }) {
  return (
    <Alert variant="destructive">
      {title ? <AlertTitle>{title}</AlertTitle> : null}
      <AlertDescription>{description}</AlertDescription>
    </Alert>
  )
}
