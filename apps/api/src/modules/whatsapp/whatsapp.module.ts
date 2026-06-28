import { Module } from '@nestjs/common'
import { WhatsAppController } from './whatsapp.controller.js'
import { WhatsAppService } from './whatsapp.service.js'

@Module({
  controllers: [WhatsAppController],
  providers: [WhatsAppService],
})
export class WhatsAppModule {}
