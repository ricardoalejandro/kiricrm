import { Controller, Get } from '@nestjs/common'

@Controller()
export class HealthController {
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
