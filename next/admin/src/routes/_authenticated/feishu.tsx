import { createFileRoute } from '@tanstack/react-router'
import { ChatBotPage } from '@/features/chat/chat-bot-page'

export const Route = createFileRoute('/_authenticated/feishu')({ component: FeishuPage })

function FeishuPage() {
  return <ChatBotPage platform="feishu" />
}
