import { Controller, Get } from '@nestjs/common'

@Controller('hello')
export class HelloController {
  @Get()
  hello() {
    return {
      success: true,
      message: 'hola mundo',
    }
  }
}
