import { Controller, Get } from '@nestjs/common'
import { PublicRoute } from './security/public-route.decorator.js'

@Controller()
export class HealthController {
  @PublicRoute()
  @Get('health')
  health() {
    return {
      success: true,
      service: 'kiricrm-api',
      status: 'ok',
      timestamp: new Date().toISOString(),
    }
  }
}
