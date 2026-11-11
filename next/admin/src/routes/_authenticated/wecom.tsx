import { createFileRoute } from '@tanstack/react-router'
import { ChatBotPage } from '@/features/chat/chat-bot-page'

export const Route = createFileRoute('/_authenticated/wecom')({ component: WecomPage })

function WecomPage() {
  return <ChatBotPage platform="wecom" />
}
