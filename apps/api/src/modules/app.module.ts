import { Module } from '@nestjs/common'
import { APP_GUARD } from '@nestjs/core'
import { DevTokenGuard } from './security/dev-token.guard.js'
import { HelloController } from './hello.controller.js'
import { HealthController } from './health.controller.js'
import { PrismaModule } from './prisma/prisma.module.js'
import { WhatsAppModule } from './whatsapp/whatsapp.module.js'

@Module({
  imports: [PrismaModule, WhatsAppModule],
  controllers: [HealthController, HelloController],
  providers: [
    {
      provide: APP_GUARD,
      useClass: DevTokenGuard,
    },
  ],
})
export class AppModule {}
