import {
  BadRequestException,
  Body,
  Controller,
  ForbiddenException,
  Get,
  Headers,
  HttpCode,
  Param,
  Patch,
  Post,
  Delete,
  Query,
  Req,
  Res,
  UnauthorizedException,
} from '@nestjs/common'
import { z } from 'zod'
import { PublicRoute } from '../security/public-route.decorator.js'
import { WhatsAppService } from './whatsapp.service.js'

type WebhookRequest = {
  rawBody?: Buffer
}

type TextReply = {
  type(contentType: string): {
    send(payload: string): unknown
  }
}

const manualConnectionSchema = z.object({
  label: z.string().trim().min(1).max(120).optional(),
  meta_business_id: z.string().trim().min(1).max(120).optional(),
  waba_id: z.string().trim().min(1).max(120),
  phone_number_id: z.string().trim().min(1).max(120),
  display_phone_number: z.string().trim().min(1).max(60).optional(),
  verified_name: z.string().trim().min(1).max(160).optional(),
  access_token: z.string().trim().min(20).optional(),
  sending_enabled: z.boolean().optional(),
  templates_enabled: z.boolean().optional(),
  capabilities: z.unknown().optional(),
})

const connectionUpdateSchema = z.object({
  label: z.string().trim().min(1).max(120).optional(),
  status: z.enum(['PENDING', 'ACTIVE', 'PAUSED', 'DISCONNECTED', 'ERROR']).optional(),
  webhook_status: z.string().trim().min(1).max(80).optional(),
  sending_enabled: z.boolean().optional(),
  templates_enabled: z.boolean().optional(),
})

const replySchema = z.object({
  text: z.string().trim().min(1).max(4096),
})

const templateButtonSchema = z.object({
  type: z.enum(['QUICK_REPLY', 'URL', 'PHONE_NUMBER']).default('QUICK_REPLY'),
  text: z.string().trim().min(1).max(25),
  url: z.string().url().optional(),
  phone_number: z.string().trim().min(5).max(30).optional(),
})

const templateCreateSchema = z.object({
  connection_id: z.string().uuid().optional(),
  name: z.string().trim().min(3).max(512).regex(/^[a-z0-9_]+$/),
  category: z.enum(['MARKETING', 'UTILITY', 'AUTHENTICATION']),
  language: z.string().trim().min(2).max(12).default('es'),
  body: z.string().trim().min(1).max(1024),
  footer: z.string().trim().max(60).optional(),
  buttons: z.array(templateButtonSchema).max(3).optional(),
  variables: z.record(z.string(), z.string()).optional(),
})

const templateSyncSchema = z.object({
  connection_id: z.string().uuid().optional(),
})

function parseBody<T>(schema: z.ZodType<T>, body: unknown): T {
  const parsed = schema.safeParse(body)

  if (!parsed.success) {
    throw new BadRequestException({
      success: false,
      error: 'Datos invalidos',
      details: parsed.error.flatten(),
    })
  }

  return parsed.data
}

function parseLimit(value: string | undefined, fallback: number, max: number) {
  const parsed = Number(value)
  if (!Number.isInteger(parsed) || parsed <= 0) return fallback
  return Math.min(parsed, max)
}

@Controller()
export class WhatsAppController {
  constructor(private readonly whatsapp: WhatsAppService) {}

  @Get('whatsapp/provider-status')
  providerStatus() {
    return {
      success: true,
      provider: this.whatsapp.providerStatus(),
    }
  }

  @Get('whatsapp/overview')
  async overview() {
    return {
      success: true,
      overview: await this.whatsapp.overview(),
    }
  }

  @Get('whatsapp/connections')
  async listConnections() {
    return {
      success: true,
      connections: await this.whatsapp.listConnections(),
    }
  }

  @Post('whatsapp/connections/manual')
  @HttpCode(200)
  async createManualConnection(@Body() body: unknown) {
    const input = parseBody(manualConnectionSchema, body)
    return this.whatsapp.createManualConnection(input)
  }

  @Patch('whatsapp/connections/:id')
  async updateConnection(@Param('id') id: string, @Body() body: unknown) {
    const input = parseBody(connectionUpdateSchema, body)
    return this.whatsapp.updateConnection(id, input)
  }

  @Delete('whatsapp/connections/:id')
  async disconnectConnection(@Param('id') id: string) {
    return this.whatsapp.disconnectConnection(id)
  }

  @Get('whatsapp/webhook-events')
  async listWebhookEvents(@Query('limit') limit?: string) {
    return {
      success: true,
      events: await this.whatsapp.listWebhookEvents(parseLimit(limit, 50, 200)),
    }
  }

  @Get('whatsapp/conversations')
  async listConversations(
    @Query('limit') limit?: string,
    @Query('search') search?: string,
    @Query('status') status?: string,
    @Query('window') windowState?: string,
  ) {
    return {
      success: true,
      conversations: await this.whatsapp.listConversations({
        limit: parseLimit(limit, 50, 200),
        search,
        status,
        windowState,
      }),
    }
  }

  @Get('whatsapp/conversations/:id/messages')
  async listConversationMessages(@Param('id') id: string, @Query('limit') limit?: string) {
    return this.whatsapp.listConversationMessages(id, parseLimit(limit, 50, 200))
  }

  @Post('whatsapp/conversations/:id/reply')
  @HttpCode(200)
  async replyToConversation(@Param('id') id: string, @Body() body: unknown) {
    const input = parseBody(replySchema, body)
    return this.whatsapp.replyToConversation(id, input.text)
  }

  @Get('whatsapp/templates')
  async listTemplates(@Query('connection_id') connectionId?: string) {
    return {
      success: true,
      templates: await this.whatsapp.listTemplates(connectionId),
    }
  }

  @Post('whatsapp/templates')
  @HttpCode(200)
  async createTemplate(@Body() body: unknown) {
    const input = parseBody(templateCreateSchema, body)
    return this.whatsapp.createTemplate(input)
  }

  @Post('whatsapp/templates/sync')
  @HttpCode(200)
  async syncTemplates(@Body() body: unknown) {
    const input = parseBody(templateSyncSchema, body || {})
    return this.whatsapp.syncTemplates(input.connection_id)
  }

  @Post('whatsapp/templates/:id/submit')
  @HttpCode(200)
  async submitTemplate(@Param('id') id: string) {
    return this.whatsapp.submitTemplate(id)
  }

  @PublicRoute()
  @Get('webhooks/whatsapp')
  verifyWebhook(@Query() query: Record<string, string | undefined>, @Res() reply: TextReply) {
    const result = this.whatsapp.verifyWebhookChallenge(query)

    if (!result.success) {
      throw new ForbiddenException({
        success: false,
        error: result.error,
      })
    }

    return reply.type('text/plain').send(result.challenge)
  }

  @PublicRoute()
  @Post('webhooks/whatsapp')
  @HttpCode(200)
  async receiveWebhook(
    @Req() request: WebhookRequest,
    @Headers('x-hub-signature-256') signature: string | undefined,
    @Body() body: unknown,
  ) {
    const result = await this.whatsapp.receiveWebhook({
      rawBody: request.rawBody,
      signature,
      body,
    })

    if (!result.success && result.reason === 'signature') {
      throw new UnauthorizedException({
        success: false,
        error: result.error,
      })
    }

    return result
  }
}
