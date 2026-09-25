import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { AppProviders } from '@/app/providers'
import { createAppRouter } from '@/app/router'
import { createAppQueryClient } from '@/app/query-client'
import { createApiClient } from '@/shared/api/client'
import { captureInvitationAuthority, capturePasswordResetAuthority, captureSetupAuthority } from '@/shared/bootstrap/setup-authority'
import { initializeI18n } from '@/shared/i18n'
import { initializeTheme } from '@/shared/theme'
import './index.css'

async function bootstrap() {
	initializeTheme()
	captureSetupAuthority()
  capturePasswordResetAuthority()
  captureInvitationAuthority()
  await initializeI18n()
  const queryClient = createAppQueryClient()
  const api = createApiClient()
  const router = createAppRouter({ api, queryClient })
  createRoot(document.getElementById('root')!).render(
    <StrictMode>
      <AppProviders queryClient={queryClient} router={router} api={api} />
    </StrictMode>,
  )
}

void bootstrap()
