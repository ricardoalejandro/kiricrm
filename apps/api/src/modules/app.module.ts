import { Module } from '@nestjs/common'
import { HelloController } from './hello.controller.js'
import { HealthController } from './health.controller.js'

@Module({
  controllers: [HealthController, HelloController],
})
export class AppModule {}
