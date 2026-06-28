import {
  createCipheriv,
  createDecipheriv,
  createHash,
  createHmac,
  randomBytes,
  timingSafeEqual,
} from 'node:crypto'
import { Injectable } from '@nestjs/common'
import { Prisma, type WhatsAppConnection } from '@prisma/client'
import { loadRuntimeEnv } from '../../config/env.js'
import { PrismaService } from '../prisma/prisma.service.js'

type JsonRecord = Record<string, unknown>

type ManualConnectionInput = {
  label?: string
  meta_business_id?: string
  waba_id: string
  phone_number_id: string
  display_phone_number?: string
  verified_name?: string
  access_token?: string
  sending_enabled?: boolean
  templates_enabled?: boolean
  capabilities?: unknown
}

type ConnectionUpdateInput = {
  label?: string
  status?: 'PENDING' | 'ACTIVE' | 'PAUSED' | 'DISCONNECTED' | 'ERROR'
  webhook_status?: string
  sending_enabled?: boolean
  templates_enabled?: boolean
}

type ConversationListInput = {
  limit: number
  search?: string
  status?: string
  windowState?: string
}

type TemplateInput = {
  connection_id?: string
  name: string
  category: 'MARKETING' | 'UTILITY' | 'AUTHENTICATION'
  language: string
  body: string
  footer?: string
  buttons?: Array<{ type: 'QUICK_REPLY' | 'URL' | 'PHONE_NUMBER'; text: string; url?: string; phone_number?: string }>
  variables?: Record<string, string>
}

type WebhookItem = {
  eventKey: string
  eventType: 'message' | 'status' | 'change'
  phoneNumberId: string | null
  payload: JsonRecord
}

type GraphResult<T> =
  | { ok: true; data: T }
  | { ok: false; status: number; error: string; data: unknown }

const SERVICE_WINDOW_MS = 24 * 60 * 60 * 1000

@Injectable()
export class WhatsAppService {
  private readonly env = loadRuntimeEnv()

  constructor(private readonly prisma: PrismaService) {}

  providerStatus() {
    const publicUrl = this.env.PUBLIC_URL.replace(/\/$/, '')

    return {
      graph_api_version: this.env.META_GRAPH_API_VERSION,
      webhook_url: `${publicUrl}/api/webhooks/whatsapp`,
      webhook_verify_token_configured: Boolean(this.env.WA_WEBHOOK_VERIFY_TOKEN),
      webhook_signature_configured: Boolean(this.env.WA_WEBHOOK_APP_SECRET),
      token_encryption_configured: Boolean(this.encryptionKey()),
      sending_enabled_global: this.env.WA_SENDING_ENABLED,
      templates_enabled_global: this.env.WA_TEMPLATES_ENABLED,
      default_daily_limit: this.env.WA_DEFAULT_DAILY_LIMIT,
      default_rate_limit_per_second: this.env.WA_DEFAULT_RATE_LIMIT_PER_SECOND,
      billing_model: 'customer_managed',
      notes: [
        'El webhook publico solo acepta POST firmados por Meta.',
        'Los envios reales quedan bloqueados mientras WA_SENDING_ENABLED sea false.',
        'Embedded Signup queda fuera de este primer corte; la conexion se registra manualmente.',
      ],
    }
  }

  async overview() {
    const account = await this.devAccount()
    const [
      connectionsCount,
      activeConnectionsCount,
      webhookEventsCount,
      conversationsCount,
      messagesCount,
      templatesCount,
      lastEvent,
      lastMessage,
    ] = await Promise.all([
      this.prisma.whatsAppConnection.count({ where: { accountId: account.id } }),
      this.prisma.whatsAppConnection.count({ where: { accountId: account.id, status: 'ACTIVE' } }),
      this.prisma.whatsAppWebhookEvent.count({ where: { accountId: account.id } }),
      this.prisma.whatsAppConversation.count({ where: { accountId: account.id } }),
      this.prisma.whatsAppMessage.count({ where: { accountId: account.id } }),
      this.prisma.whatsAppTemplate.count({ where: { accountId: account.id } }),
      this.prisma.whatsAppWebhookEvent.findFirst({
        where: { accountId: account.id },
        orderBy: { receivedAt: 'desc' },
        select: { receivedAt: true, status: true, eventType: true },
      }),
      this.prisma.whatsAppMessage.findFirst({
        where: { accountId: account.id },
        orderBy: { createdAt: 'desc' },
        select: { createdAt: true, direction: true, status: true, messageType: true },
      }),
    ])

    return {
      account: { id: account.id, slug: account.slug, name: account.name },
      connections_count: connectionsCount,
      active_connections_count: activeConnectionsCount,
      webhook_events_count: webhookEventsCount,
      conversations_count: conversationsCount,
      messages_count: messagesCount,
      templates_count: templatesCount,
      last_event: lastEvent
        ? {
            received_at: lastEvent.receivedAt.toISOString(),
            status: lastEvent.status,
            event_type: lastEvent.eventType,
          }
        : null,
      last_message: lastMessage
        ? {
            created_at: lastMessage.createdAt.toISOString(),
            direction: lastMessage.direction,
            status: lastMessage.status,
            message_type: lastMessage.messageType,
          }
        : null,
    }
  }

  async listConnections() {
    const account = await this.devAccount()
    const connections = await this.prisma.whatsAppConnection.findMany({
      where: { accountId: account.id },
      orderBy: { updatedAt: 'desc' },
      include: {
        _count: {
          select: {
            webhookEvents: true,
            conversations: true,
            messages: true,
            templates: true,
          },
        },
      },
    })

    return connections.map((connection) => this.connectionDto(connection))
  }

  async createManualConnection(input: ManualConnectionInput) {
    const account = await this.devAccount()
    const warnings: string[] = []
    const tokenCipher = input.access_token ? this.encryptTokenForStorage(input.access_token) : undefined

    if (input.sending_enabled && !this.env.WA_SENDING_ENABLED) {
      warnings.push('WA_SENDING_ENABLED esta en false; la conexion se crea con envio real apagado.')
    }

    if (input.templates_enabled && !this.env.WA_TEMPLATES_ENABLED) {
      warnings.push('WA_TEMPLATES_ENABLED esta en false; el envio de plantillas a Meta queda apagado.')
    }

    if (input.access_token && !tokenCipher) {
      return {
        success: false as const,
        error: 'No se puede guardar access_token porque WA_TOKEN_ENCRYPTION_KEY no esta configurado correctamente.',
      }
    }

    const connection = await this.prisma.whatsAppConnection.upsert({
      where: {
        accountId_phoneNumberId: {
          accountId: account.id,
          phoneNumberId: input.phone_number_id,
        },
      },
      create: {
        accountId: account.id,
        label: input.label,
        metaBusinessId: input.meta_business_id,
        wabaId: input.waba_id,
        phoneNumberId: input.phone_number_id,
        displayPhoneNumber: input.display_phone_number,
        verifiedName: input.verified_name,
        status: 'ACTIVE',
        webhookStatus: 'manual_pending',
        billingStatus: 'customer_managed',
        sendingEnabled: Boolean(input.sending_enabled && this.env.WA_SENDING_ENABLED),
        templatesEnabled: Boolean(input.templates_enabled && this.env.WA_TEMPLATES_ENABLED),
        capabilities: this.toInputJson(input.capabilities),
        accessTokenCipher: tokenCipher,
        connectedAt: new Date(),
      },
      update: {
        label: input.label,
        metaBusinessId: input.meta_business_id,
        wabaId: input.waba_id,
        displayPhoneNumber: input.display_phone_number,
        verifiedName: input.verified_name,
        status: 'ACTIVE',
        disconnectedAt: null,
        sendingEnabled: Boolean(input.sending_enabled && this.env.WA_SENDING_ENABLED),
        templatesEnabled: Boolean(input.templates_enabled && this.env.WA_TEMPLATES_ENABLED),
        capabilities: this.toInputJson(input.capabilities),
        accessTokenCipher: tokenCipher,
        connectedAt: new Date(),
      },
    })

    return {
      success: true as const,
      connection: this.connectionDto(connection),
      warnings,
    }
  }

  async updateConnection(id: string, input: ConnectionUpdateInput) {
    const account = await this.devAccount()
    const connection = await this.prisma.whatsAppConnection.findFirst({
      where: { id, accountId: account.id },
    })

    if (!connection) {
      return { success: false as const, error: 'Conexion no encontrada.' }
    }

    if (input.sending_enabled && !this.env.WA_SENDING_ENABLED) {
      return { success: false as const, error: 'No se puede activar envio real porque WA_SENDING_ENABLED esta en false.' }
    }

    if (input.templates_enabled && !this.env.WA_TEMPLATES_ENABLED) {
      return { success: false as const, error: 'No se puede activar plantillas reales porque WA_TEMPLATES_ENABLED esta en false.' }
    }

    const data: Prisma.WhatsAppConnectionUpdateInput = {
      label: input.label,
      status: input.status,
      webhookStatus: input.webhook_status,
      sendingEnabled: input.sending_enabled,
      templatesEnabled: input.templates_enabled,
    }

    if (input.status === 'DISCONNECTED') {
      data.disconnectedAt = new Date()
      data.sendingEnabled = false
      data.templatesEnabled = false
    }

    if (input.status === 'ACTIVE') {
      data.connectedAt = new Date()
      data.disconnectedAt = null
    }

    const updated = await this.prisma.whatsAppConnection.update({
      where: { id: connection.id },
      data,
    })

    return { success: true as const, connection: this.connectionDto(updated) }
  }

  async disconnectConnection(id: string) {
    return this.updateConnection(id, { status: 'DISCONNECTED', sending_enabled: false, templates_enabled: false })
  }

  async listWebhookEvents(limit: number) {
    const account = await this.devAccount()
    const events = await this.prisma.whatsAppWebhookEvent.findMany({
      where: { accountId: account.id },
      orderBy: { receivedAt: 'desc' },
      take: limit,
      include: {
        connection: {
          select: {
            id: true,
            label: true,
            phoneNumberId: true,
            displayPhoneNumber: true,
          },
        },
      },
    })

    return events.map((event) => ({
      id: event.id,
      event_key: event.eventKey,
      event_type: event.eventType,
      phone_number_id: event.phoneNumberId,
      status: event.status,
      error: event.error,
      received_at: event.receivedAt.toISOString(),
      processed_at: event.processedAt?.toISOString() ?? null,
      connection: event.connection,
    }))
  }

  async listConversations(input: ConversationListInput) {
    const account = await this.devAccount()
    const where: Prisma.WhatsAppConversationWhereInput = {
      accountId: account.id,
    }

    if (input.status) {
      where.status = input.status
    }

    if (input.search) {
      where.OR = [
        { customerPhone: { contains: input.search, mode: 'insensitive' } },
        { customerName: { contains: input.search, mode: 'insensitive' } },
      ]
    }

    if (input.windowState === 'active') {
      where.serviceWindowExpiresAt = { gt: new Date() }
    }

    if (input.windowState === 'expired') {
      where.OR = [
        ...(where.OR ?? []),
        { serviceWindowExpiresAt: null },
        { serviceWindowExpiresAt: { lte: new Date() } },
      ]
    }

    const conversations = await this.prisma.whatsAppConversation.findMany({
      where,
      orderBy: [{ lastMessageAt: 'desc' }, { updatedAt: 'desc' }],
      take: input.limit,
      include: {
        connection: {
          select: {
            id: true,
            label: true,
            phoneNumberId: true,
            displayPhoneNumber: true,
          },
        },
        _count: {
          select: { messages: true },
        },
      },
    })

    return conversations.map((conversation) => ({
      id: conversation.id,
      status: conversation.status,
      customer_phone: conversation.customerPhone,
      customer_name: conversation.customerName,
      phone_number_id: conversation.phoneNumberId,
      unread_count: conversation.unreadCount,
      messages_count: conversation._count.messages,
      last_message_at: conversation.lastMessageAt?.toISOString() ?? null,
      last_inbound_at: conversation.lastInboundAt?.toISOString() ?? null,
      last_outbound_at: conversation.lastOutboundAt?.toISOString() ?? null,
      service_window_expires_at: conversation.serviceWindowExpiresAt?.toISOString() ?? null,
      connection: conversation.connection,
    }))
  }

  async listConversationMessages(id: string, limit: number) {
    const account = await this.devAccount()
    const conversation = await this.prisma.whatsAppConversation.findFirst({
      where: { id, accountId: account.id },
      include: {
        connection: {
          select: {
            id: true,
            label: true,
            phoneNumberId: true,
            displayPhoneNumber: true,
          },
        },
      },
    })

    if (!conversation) {
      return { success: false as const, error: 'Conversacion no encontrada.' }
    }

    const messages = await this.prisma.whatsAppMessage.findMany({
      where: { accountId: account.id, conversationId: conversation.id },
      orderBy: { createdAt: 'asc' },
      take: limit,
    })

    return {
      success: true as const,
      conversation: {
        id: conversation.id,
        status: conversation.status,
        customer_phone: conversation.customerPhone,
        customer_name: conversation.customerName,
        service_window_expires_at: conversation.serviceWindowExpiresAt?.toISOString() ?? null,
        connection: conversation.connection,
      },
      messages: messages.map((message) => this.messageDto(message)),
    }
  }

  async replyToConversation(id: string, text: string) {
    const account = await this.devAccount()
    const conversation = await this.prisma.whatsAppConversation.findFirst({
      where: { id, accountId: account.id },
      include: { connection: true },
    })

    if (!conversation) {
      return { success: false as const, error: 'Conversacion no encontrada.' }
    }

    if (!this.env.WA_SENDING_ENABLED) {
      await this.createOutboundJob(account.id, conversation.connectionId, 'reply_text_blocked', { conversation_id: id, text })
      return {
        success: false as const,
        error: 'Envio real deshabilitado por WA_SENDING_ENABLED=false. Se registro la intencion en outbound_jobs.',
      }
    }

    if (!conversation.connection || !conversation.connection.sendingEnabled) {
      return { success: false as const, error: 'La conexion no tiene envio real habilitado.' }
    }

    if (!conversation.serviceWindowExpiresAt || conversation.serviceWindowExpiresAt.getTime() <= Date.now()) {
      return { success: false as const, error: 'La ventana de 24 horas expiro; usa una plantilla aprobada.' }
    }

    const token = this.tokenForConnection(conversation.connection)
    if (!token) {
      return { success: false as const, error: 'La conexion no tiene access_token cifrado utilizable.' }
    }

    const graph = await this.graphRequest<JsonRecord>('POST', `/${conversation.connection.phoneNumberId}/messages`, token, {
      messaging_product: 'whatsapp',
      to: conversation.customerPhone,
      type: 'text',
      text: { body: text },
    })

    if (!graph.ok) {
      await this.createOutboundJob(account.id, conversation.connectionId, 'reply_text_failed', {
        conversation_id: id,
        text,
        graph_error: graph.error,
      })
      return { success: false as const, error: graph.error }
    }

    const messageId = this.firstMessageId(graph.data)
    const now = new Date()
    const message = await this.prisma.whatsAppMessage.create({
      data: {
        accountId: account.id,
        connectionId: conversation.connection.id,
        conversationId: conversation.id,
        externalMessageId: messageId,
        direction: 'outbound',
        messageType: 'text',
        text,
        status: 'sent',
        payload: this.toInputJson(graph.data) ?? {},
        sentAt: now,
      },
    })

    await this.prisma.whatsAppConversation.update({
      where: { id: conversation.id },
      data: {
        lastMessageAt: now,
        lastOutboundAt: now,
      },
    })

    return { success: true as const, message: this.messageDto(message) }
  }

  async listTemplates(connectionId?: string) {
    const account = await this.devAccount()
    const templates = await this.prisma.whatsAppTemplate.findMany({
      where: {
        accountId: account.id,
        connectionId,
      },
      orderBy: { updatedAt: 'desc' },
      include: {
        connection: {
          select: {
            id: true,
            label: true,
            phoneNumberId: true,
            displayPhoneNumber: true,
          },
        },
      },
    })

    return templates.map((template) => ({
      id: template.id,
      connection_id: template.connectionId,
      waba_id: template.wabaId,
      name: template.name,
      language: template.language,
      category: template.category,
      status: template.status,
      components: template.components,
      variables: template.variables,
      external_id: template.externalId,
      quality_rating: template.qualityRating,
      rejection_reason: template.rejectionReason,
      updated_at: template.updatedAt.toISOString(),
      connection: template.connection,
    }))
  }

  async createTemplate(input: TemplateInput) {
    const account = await this.devAccount()
    const connection = input.connection_id
      ? await this.prisma.whatsAppConnection.findFirst({ where: { id: input.connection_id, accountId: account.id } })
      : null

    if (input.connection_id && !connection) {
      return { success: false as const, error: 'Conexion no encontrada para la plantilla.' }
    }

    const template = await this.prisma.whatsAppTemplate.upsert({
      where: {
        accountId_name_language: {
          accountId: account.id,
          name: input.name,
          language: input.language,
        },
      },
      create: {
        accountId: account.id,
        connectionId: connection?.id,
        wabaId: connection?.wabaId,
        name: input.name,
        language: input.language,
        category: input.category,
        status: 'draft',
        components: this.toInputJson(this.templateComponents(input)) ?? {},
        variables: this.toInputJson(input.variables),
      },
      update: {
        connectionId: connection?.id,
        wabaId: connection?.wabaId,
        category: input.category,
        status: 'draft',
        components: this.toInputJson(this.templateComponents(input)) ?? {},
        variables: this.toInputJson(input.variables),
      },
    })

    return {
      success: true as const,
      template: {
        id: template.id,
        name: template.name,
        language: template.language,
        category: template.category,
        status: template.status,
      },
    }
  }

  async syncTemplates(connectionId?: string) {
    const account = await this.devAccount()
    const connection = await this.resolveConnection(account.id, connectionId)

    if (!connection) {
      return { success: false as const, error: 'No hay conexion WhatsApp activa para sincronizar plantillas.' }
    }

    const token = this.tokenForConnection(connection)
    if (!token) {
      return { success: false as const, error: 'La conexion no tiene access_token cifrado utilizable.' }
    }

    const graph = await this.graphRequest<{ data?: JsonRecord[] }>(
      'GET',
      `/${connection.wabaId}/message_templates?fields=name,language,status,category,components,quality_score,rejected_reason`,
      token,
    )

    if (!graph.ok) {
      return { success: false as const, error: graph.error }
    }

    const templates = Array.isArray(graph.data.data) ? graph.data.data : []
    let synced = 0

    for (const remote of templates) {
      const name = this.stringValue(remote.name)
      const language = this.stringValue(remote.language)
      if (!name || !language) continue

      const quality = this.asRecord(remote.quality_score)
      await this.prisma.whatsAppTemplate.upsert({
        where: {
          accountId_name_language: {
            accountId: account.id,
            name,
            language,
          },
        },
        create: {
          accountId: account.id,
          connectionId: connection.id,
          wabaId: connection.wabaId,
          name,
          language,
          category: this.stringValue(remote.category) ?? 'UNKNOWN',
          status: this.stringValue(remote.status) ?? 'UNKNOWN',
          components: this.toInputJson(remote.components) ?? [],
          externalId: this.stringValue(remote.id),
          qualityRating: this.stringValue(quality.score),
          rejectionReason: this.stringValue(remote.rejected_reason),
        },
        update: {
          connectionId: connection.id,
          wabaId: connection.wabaId,
          category: this.stringValue(remote.category) ?? 'UNKNOWN',
          status: this.stringValue(remote.status) ?? 'UNKNOWN',
          components: this.toInputJson(remote.components) ?? [],
          externalId: this.stringValue(remote.id),
          qualityRating: this.stringValue(quality.score),
          rejectionReason: this.stringValue(remote.rejected_reason),
        },
      })
      synced += 1
    }

    return { success: true as const, synced }
  }

  async submitTemplate(id: string) {
    const account = await this.devAccount()

    if (!this.env.WA_TEMPLATES_ENABLED) {
      return { success: false as const, error: 'Envio real de plantillas deshabilitado por WA_TEMPLATES_ENABLED=false.' }
    }

    const template = await this.prisma.whatsAppTemplate.findFirst({
      where: { id, accountId: account.id },
      include: { connection: true },
    })

    if (!template) {
      return { success: false as const, error: 'Plantilla no encontrada.' }
    }

    const connection = template.connection ?? (await this.resolveConnection(account.id))
    if (!connection || !connection.templatesEnabled) {
      return { success: false as const, error: 'No hay conexion con plantillas reales habilitadas.' }
    }

    const token = this.tokenForConnection(connection)
    if (!token) {
      return { success: false as const, error: 'La conexion no tiene access_token cifrado utilizable.' }
    }

    const graph = await this.graphRequest<JsonRecord>('POST', `/${connection.wabaId}/message_templates`, token, {
      name: template.name,
      language: template.language,
      category: template.category,
      components: template.components,
    })

    if (!graph.ok) {
      await this.prisma.whatsAppTemplate.update({
        where: { id: template.id },
        data: {
          status: 'submit_failed',
          rejectionReason: graph.error,
        },
      })
      return { success: false as const, error: graph.error }
    }

    const updated = await this.prisma.whatsAppTemplate.update({
      where: { id: template.id },
      data: {
        connectionId: connection.id,
        wabaId: connection.wabaId,
        status: 'submitted',
        externalId: this.stringValue(graph.data.id),
        rejectionReason: null,
      },
    })

    return {
      success: true as const,
      template: {
        id: updated.id,
        name: updated.name,
        language: updated.language,
        status: updated.status,
        external_id: updated.externalId,
      },
    }
  }

  verifyWebhookChallenge(query: Record<string, string | undefined>) {
    const mode = query['hub.mode']
    const token = query['hub.verify_token']
    const challenge = query['hub.challenge']

    if (mode !== 'subscribe' || !token || !challenge) {
      return { success: false as const, error: 'Parametros de verificacion invalidos.' }
    }

    if (!this.env.WA_WEBHOOK_VERIFY_TOKEN || !this.safeCompare(this.env.WA_WEBHOOK_VERIFY_TOKEN, token)) {
      return { success: false as const, error: 'Verify token incorrecto.' }
    }

    return { success: true as const, challenge }
  }

  async receiveWebhook(input: { rawBody?: Buffer; signature?: string; body: unknown }) {
    const rawBody = input.rawBody ?? Buffer.from(JSON.stringify(input.body ?? {}))

    if (!this.verifyMetaSignature(rawBody, input.signature)) {
      return {
        success: false as const,
        reason: 'signature' as const,
        error: 'Firma x-hub-signature-256 invalida o no configurada.',
      }
    }

    const items = this.extractWebhookItems(input.body)
    const processed = []

    for (const item of items) {
      processed.push(await this.storeWebhookItem(item))
    }

    return {
      success: true as const,
      received: items.length,
      processed,
    }
  }

  private async devAccount() {
    return this.prisma.account.upsert({
      where: { slug: this.env.WA_DEV_ACCOUNT_SLUG },
      create: {
        slug: this.env.WA_DEV_ACCOUNT_SLUG,
        name: this.env.WA_DEV_ACCOUNT_NAME,
      },
      update: {
        name: this.env.WA_DEV_ACCOUNT_NAME,
      },
    })
  }

  private async resolveConnection(accountId: string, connectionId?: string) {
    if (connectionId) {
      return this.prisma.whatsAppConnection.findFirst({
        where: { id: connectionId, accountId },
      })
    }

    return this.prisma.whatsAppConnection.findFirst({
      where: { accountId, status: 'ACTIVE' },
      orderBy: { updatedAt: 'desc' },
    })
  }

  private async createOutboundJob(accountId: string, connectionId: string | null, kind: string, payload: unknown) {
    await this.prisma.whatsAppOutboundJob.create({
      data: {
        accountId,
        connectionId,
        kind,
        status: 'blocked',
        payload: this.toInputJson(payload) ?? {},
      },
    })
  }

  private async storeWebhookItem(item: WebhookItem) {
    const connection = item.phoneNumberId
      ? await this.prisma.whatsAppConnection.findFirst({
          where: {
            phoneNumberId: item.phoneNumberId,
            status: { not: 'DISCONNECTED' },
          },
          orderBy: { updatedAt: 'desc' },
        })
      : null

    const event = await this.prisma.whatsAppWebhookEvent.upsert({
      where: { eventKey: item.eventKey },
      create: {
        accountId: connection?.accountId,
        connectionId: connection?.id,
        eventKey: item.eventKey,
        eventType: item.eventType,
        phoneNumberId: item.phoneNumberId,
        status: 'RECEIVED',
        payload: this.toInputJson(item.payload) ?? {},
      },
      update: {},
    })

    if (event.status === 'PROCESSED' || event.status === 'IGNORED') {
      return { event_key: item.eventKey, status: event.status.toLowerCase(), idempotent: true }
    }

    if (!connection) {
      const status = item.eventType === 'change' ? 'IGNORED' : 'FAILED'
      const error = item.eventType === 'change' ? null : 'No existe conexion activa para este phone_number_id.'
      await this.prisma.whatsAppWebhookEvent.update({
        where: { id: event.id },
        data: {
          status,
          error,
          processedAt: new Date(),
        },
      })
      return { event_key: item.eventKey, status: status.toLowerCase(), error }
    }

    try {
      if (item.eventType === 'message') {
        await this.processInboundMessage(connection, item)
      } else if (item.eventType === 'status') {
        await this.processMessageStatus(connection, item)
      }

      await this.prisma.whatsAppWebhookEvent.update({
        where: { id: event.id },
        data: {
          accountId: connection.accountId,
          connectionId: connection.id,
          status: 'PROCESSED',
          error: null,
          processedAt: new Date(),
        },
      })
      return { event_key: item.eventKey, status: 'processed' }
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Error procesando webhook.'
      await this.prisma.whatsAppWebhookEvent.update({
        where: { id: event.id },
        data: {
          accountId: connection.accountId,
          connectionId: connection.id,
          status: 'FAILED',
          error: message,
          processedAt: new Date(),
        },
      })
      return { event_key: item.eventKey, status: 'failed', error: message }
    }
  }

  private async processInboundMessage(connection: WhatsAppConnection, item: WebhookItem) {
    const message = this.asRecord(item.payload.message)
    const value = this.asRecord(item.payload.value)
    const contacts = this.arrayRecords(value.contacts)
    const contact = contacts.find((candidate) => this.stringValue(candidate.wa_id) === this.stringValue(message.from)) ?? contacts[0]
    const profile = this.asRecord(contact?.profile)
    const customerPhone = this.stringValue(message.from)
    const externalMessageId = this.stringValue(message.id) ?? item.eventKey

    if (!customerPhone) {
      throw new Error('Webhook message sin remitente.')
    }

    const timestamp = this.timestampToDate(message.timestamp)
    const customerName = this.stringValue(profile.name)
    const messageType = this.stringValue(message.type) ?? 'unknown'
    const text = this.extractMessageText(message, messageType)

    const conversation = await this.prisma.whatsAppConversation.upsert({
      where: {
        accountId_phoneNumberId_customerPhone: {
          accountId: connection.accountId,
          phoneNumberId: connection.phoneNumberId,
          customerPhone,
        },
      },
      create: {
        accountId: connection.accountId,
        connectionId: connection.id,
        phoneNumberId: connection.phoneNumberId,
        customerPhone,
        customerName,
        status: 'open',
        unreadCount: 1,
        lastMessageAt: timestamp,
        lastInboundAt: timestamp,
        serviceWindowExpiresAt: new Date(timestamp.getTime() + SERVICE_WINDOW_MS),
      },
      update: {
        connectionId: connection.id,
        customerName: customerName || undefined,
        status: 'open',
        unreadCount: { increment: 1 },
        lastMessageAt: timestamp,
        lastInboundAt: timestamp,
        serviceWindowExpiresAt: new Date(timestamp.getTime() + SERVICE_WINDOW_MS),
      },
    })

    await this.prisma.whatsAppMessage.upsert({
      where: { externalMessageId },
      create: {
        accountId: connection.accountId,
        connectionId: connection.id,
        conversationId: conversation.id,
        externalMessageId,
        direction: 'inbound',
        messageType,
        text,
        status: 'received',
        payload: this.toInputJson(item.payload) ?? {},
      },
      update: {
        accountId: connection.accountId,
        connectionId: connection.id,
        conversationId: conversation.id,
        messageType,
        text,
        status: 'received',
        payload: this.toInputJson(item.payload) ?? {},
      },
    })
  }

  private async processMessageStatus(connection: WhatsAppConnection, item: WebhookItem) {
    const statusPayload = this.asRecord(item.payload.status)
    const externalMessageId = this.stringValue(statusPayload.id)
    const status = this.stringValue(statusPayload.status) ?? 'unknown'

    if (!externalMessageId) {
      throw new Error('Webhook status sin id de mensaje.')
    }

    const timestamp = this.timestampToDate(statusPayload.timestamp)
    const update: Prisma.WhatsAppMessageUpdateManyMutationInput = {
      status,
      payload: this.toInputJson(item.payload) ?? {},
    }

    if (status === 'sent') update.sentAt = timestamp
    if (status === 'delivered') update.deliveredAt = timestamp
    if (status === 'read') update.readAt = timestamp
    if (status === 'failed') update.failedAt = timestamp

    await this.prisma.whatsAppMessage.updateMany({
      where: {
        accountId: connection.accountId,
        externalMessageId,
      },
      data: update,
    })
  }

  private extractWebhookItems(payload: unknown): WebhookItem[] {
    const root = this.asRecord(payload)
    const entries = this.arrayRecords(root.entry)
    const items: WebhookItem[] = []

    for (const entry of entries) {
      const changes = this.arrayRecords(entry.changes)

      for (const change of changes) {
        const value = this.asRecord(change.value)
        const metadata = this.asRecord(value.metadata)
        const phoneNumberId = this.stringValue(metadata.phone_number_id) ?? null
        const messages = this.arrayRecords(value.messages)
        const statuses = this.arrayRecords(value.statuses)

        for (const message of messages) {
          const messageId = this.stringValue(message.id) ?? this.hash(message)
          items.push({
            eventKey: `message:${messageId}`,
            eventType: 'message',
            phoneNumberId,
            payload: {
              entry_id: this.stringValue(entry.id),
              field: this.stringValue(change.field),
              value,
              metadata,
              contacts: value.contacts,
              message,
            },
          })
        }

        for (const status of statuses) {
          const statusId = this.stringValue(status.id) ?? this.hash(status)
          const statusName = this.stringValue(status.status) ?? 'unknown'
          const timestamp = this.stringValue(status.timestamp) ?? 'no_timestamp'
          items.push({
            eventKey: `status:${statusId}:${statusName}:${timestamp}`,
            eventType: 'status',
            phoneNumberId,
            payload: {
              entry_id: this.stringValue(entry.id),
              field: this.stringValue(change.field),
              value,
              metadata,
              status,
            },
          })
        }

        if (messages.length === 0 && statuses.length === 0) {
          items.push({
            eventKey: `change:${this.hash({ entry, change })}`,
            eventType: 'change',
            phoneNumberId,
            payload: {
              entry_id: this.stringValue(entry.id),
              field: this.stringValue(change.field),
              value,
              metadata,
            },
          })
        }
      }
    }

    return items
  }

  private templateComponents(input: TemplateInput) {
    const components: JsonRecord[] = [
      {
        type: 'BODY',
        text: input.body,
      },
    ]

    if (input.footer) {
      components.push({
        type: 'FOOTER',
        text: input.footer,
      })
    }

    if (input.buttons?.length) {
      components.push({
        type: 'BUTTONS',
        buttons: input.buttons.map((button) => ({
          type: button.type,
          text: button.text,
          url: button.url,
          phone_number: button.phone_number,
        })),
      })
    }

    return components
  }

  private verifyMetaSignature(rawBody: Buffer, signature: string | undefined) {
    if (!this.env.WA_WEBHOOK_APP_SECRET || !signature?.startsWith('sha256=')) {
      return false
    }

    const expected = `sha256=${createHmac('sha256', this.env.WA_WEBHOOK_APP_SECRET).update(rawBody).digest('hex')}`
    return this.safeCompare(expected, signature)
  }

  private safeCompare(expected: string, received: string) {
    const expectedBuffer = Buffer.from(expected)
    const receivedBuffer = Buffer.from(received)

    if (expectedBuffer.length !== receivedBuffer.length) {
      return false
    }

    return timingSafeEqual(expectedBuffer, receivedBuffer)
  }

  private encryptTokenForStorage(token: string) {
    const key = this.encryptionKey()
    if (!key) return null

    const iv = randomBytes(12)
    const cipher = createCipheriv('aes-256-gcm', key, iv)
    const encrypted = Buffer.concat([cipher.update(token, 'utf8'), cipher.final()])
    const tag = cipher.getAuthTag()

    return ['v1', iv.toString('base64url'), tag.toString('base64url'), encrypted.toString('base64url')].join(':')
  }

  private tokenForConnection(connection: Pick<WhatsAppConnection, 'accessTokenCipher'>) {
    if (!connection.accessTokenCipher) return null

    const key = this.encryptionKey()
    if (!key) return null

    const [version, iv, tag, encrypted] = connection.accessTokenCipher.split(':')
    if (version !== 'v1' || !iv || !tag || !encrypted) return null

    try {
      const decipher = createDecipheriv('aes-256-gcm', key, Buffer.from(iv, 'base64url'))
      decipher.setAuthTag(Buffer.from(tag, 'base64url'))
      return Buffer.concat([
        decipher.update(Buffer.from(encrypted, 'base64url')),
        decipher.final(),
      ]).toString('utf8')
    } catch {
      return null
    }
  }

  private encryptionKey() {
    if (!this.env.WA_TOKEN_ENCRYPTION_KEY) return null

    try {
      const key = Buffer.from(this.env.WA_TOKEN_ENCRYPTION_KEY, 'base64')
      return key.length === 32 ? key : null
    } catch {
      return null
    }
  }

  private async graphRequest<T>(method: 'GET' | 'POST', path: string, token: string, body?: unknown): Promise<GraphResult<T>> {
    const response = await fetch(`https://graph.facebook.com/${this.env.META_GRAPH_API_VERSION}${path}`, {
      method,
      headers: {
        authorization: `Bearer ${token}`,
        ...(body ? { 'content-type': 'application/json' } : {}),
      },
      body: body ? JSON.stringify(body) : undefined,
    })

    const data = await response.json().catch(() => ({}))
    if (!response.ok) {
      return {
        ok: false,
        status: response.status,
        error: this.metaErrorMessage(data),
        data,
      }
    }

    return { ok: true, data: data as T }
  }

  private metaErrorMessage(data: unknown) {
    const root = this.asRecord(data)
    const error = this.asRecord(root.error)
    return this.stringValue(error.message) ?? this.stringValue(root.message) ?? 'Meta Graph API devolvio un error.'
  }

  private firstMessageId(data: JsonRecord) {
    const messages = this.arrayRecords(data.messages)
    return this.stringValue(messages[0]?.id) ?? null
  }

  private connectionDto(
    connection: WhatsAppConnection & {
      _count?: {
        webhookEvents?: number
        conversations?: number
        messages?: number
        templates?: number
      }
    },
  ) {
    return {
      id: connection.id,
      label: connection.label,
      meta_business_id: connection.metaBusinessId,
      waba_id: connection.wabaId,
      phone_number_id: connection.phoneNumberId,
      display_phone_number: connection.displayPhoneNumber,
      verified_name: connection.verifiedName,
      status: connection.status,
      webhook_status: connection.webhookStatus,
      billing_status: connection.billingStatus,
      sending_enabled: connection.sendingEnabled,
      templates_enabled: connection.templatesEnabled,
      access_token_configured: Boolean(connection.accessTokenCipher),
      token_expires_at: connection.tokenExpiresAt?.toISOString() ?? null,
      connected_at: connection.connectedAt?.toISOString() ?? null,
      disconnected_at: connection.disconnectedAt?.toISOString() ?? null,
      counts: connection._count
        ? {
            webhook_events: connection._count.webhookEvents ?? 0,
            conversations: connection._count.conversations ?? 0,
            messages: connection._count.messages ?? 0,
            templates: connection._count.templates ?? 0,
          }
        : undefined,
      updated_at: connection.updatedAt.toISOString(),
    }
  }

  private messageDto(message: {
    id: string
    externalMessageId: string | null
    direction: string
    messageType: string
    text: string | null
    status: string
    payload: Prisma.JsonValue
    sentAt: Date | null
    deliveredAt: Date | null
    readAt: Date | null
    failedAt: Date | null
    createdAt: Date
  }) {
    return {
      id: message.id,
      external_message_id: message.externalMessageId,
      direction: message.direction,
      message_type: message.messageType,
      text: message.text,
      status: message.status,
      payload: message.payload,
      sent_at: message.sentAt?.toISOString() ?? null,
      delivered_at: message.deliveredAt?.toISOString() ?? null,
      read_at: message.readAt?.toISOString() ?? null,
      failed_at: message.failedAt?.toISOString() ?? null,
      created_at: message.createdAt.toISOString(),
    }
  }

  private extractMessageText(message: JsonRecord, type: string) {
    if (type === 'text') return this.stringValue(this.asRecord(message.text).body)
    if (type === 'button') return this.stringValue(this.asRecord(message.button).text)
    if (type === 'interactive') {
      const interactive = this.asRecord(message.interactive)
      return (
        this.stringValue(this.asRecord(interactive.button_reply).title) ??
        this.stringValue(this.asRecord(interactive.list_reply).title)
      )
    }

    const typedPayload = this.asRecord(message[type])
    return this.stringValue(typedPayload.caption) ?? `[${type}]`
  }

  private timestampToDate(value: unknown) {
    const timestamp = typeof value === 'number' ? value : Number(this.stringValue(value))
    if (!Number.isFinite(timestamp) || timestamp <= 0) return new Date()
    return new Date(timestamp * 1000)
  }

  private toInputJson(value: unknown): Prisma.InputJsonValue | undefined {
    if (value === undefined) return undefined
    return JSON.parse(JSON.stringify(value)) as Prisma.InputJsonValue
  }

  private hash(value: unknown) {
    return createHash('sha256').update(JSON.stringify(value)).digest('hex')
  }

  private asRecord(value: unknown): JsonRecord {
    return value && typeof value === 'object' && !Array.isArray(value) ? (value as JsonRecord) : {}
  }

  private arrayRecords(value: unknown): JsonRecord[] {
    return Array.isArray(value) ? value.map((item) => this.asRecord(item)).filter((item) => Object.keys(item).length > 0) : []
  }

  private stringValue(value: unknown) {
    return typeof value === 'string' && value.length > 0 ? value : undefined
  }
}
