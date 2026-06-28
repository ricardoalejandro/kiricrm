import 'reflect-metadata'
import helmet from '@fastify/helmet'
import { NestFactory } from '@nestjs/core'
import { FastifyAdapter, NestFastifyApplication } from '@nestjs/platform-fastify'
import { AppModule } from './modules/app.module.js'

const DEFAULT_PORT = 8080

function readPort() {
  const value = Number(process.env.PORT || DEFAULT_PORT)
  return Number.isInteger(value) && value > 0 ? value : DEFAULT_PORT
}

async function bootstrap() {
  const app = await NestFactory.create<NestFastifyApplication>(
    AppModule,
    new FastifyAdapter({ logger: process.env.NODE_ENV !== 'test' }),
    { bufferLogs: true },
  )

  await app.register(helmet)
  app.setGlobalPrefix('api', {
    exclude: ['health'],
  })

  await app.listen({ host: '127.0.0.1', port: readPort() })
}

bootstrap().catch((error) => {
  console.error(error)
  process.exit(1)
})
