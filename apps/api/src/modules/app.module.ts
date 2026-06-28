import { Module } from '@nestjs/common'
import { APP_GUARD } from '@nestjs/core'
import { DevTokenGuard } from './security/dev-token.guard.js'
import { HelloController } from './hello.controller.js'
import { HealthController } from './health.controller.js'

@Module({
  controllers: [HealthController, HelloController],
  providers: [
    {
      provide: APP_GUARD,
      useClass: DevTokenGuard,
    },
  ],
})
export class AppModule {}
