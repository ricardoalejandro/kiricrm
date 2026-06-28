import { timingSafeEqual } from 'node:crypto'
import {
  CanActivate,
  ExecutionContext,
  Injectable,
  ServiceUnavailableException,
  UnauthorizedException,
} from '@nestjs/common'
import { Reflector } from '@nestjs/core'
import { PUBLIC_ROUTE_METADATA } from './public-route.decorator.js'

const TOKEN_HEADER = 'x-kiri-dev-token'

type HeaderValue = string | string[] | undefined

function firstHeader(value: HeaderValue) {
  return Array.isArray(value) ? value[0] : value
}

function tokensMatch(expected: string, received: string) {
  const expectedBuffer = Buffer.from(expected)
  const receivedBuffer = Buffer.from(received)

  if (expectedBuffer.length !== receivedBuffer.length) {
    return false
  }

  return timingSafeEqual(expectedBuffer, receivedBuffer)
}

@Injectable()
export class DevTokenGuard implements CanActivate {
  constructor(private readonly reflector: Reflector) {}

  canActivate(context: ExecutionContext) {
    const isPublicRoute = this.reflector.getAllAndOverride<boolean>(PUBLIC_ROUTE_METADATA, [
      context.getHandler(),
      context.getClass(),
    ])

    if (isPublicRoute) {
      return true
    }

    const expectedToken = process.env.KIRI_DEV_API_TOKEN

    if (!expectedToken) {
      throw new ServiceUnavailableException('API token is not configured')
    }

    const request = context.switchToHttp().getRequest<{
      headers: Record<string, HeaderValue>
    }>()
    const receivedToken = firstHeader(request.headers[TOKEN_HEADER])

    if (!receivedToken || !tokensMatch(expectedToken, receivedToken)) {
      throw new UnauthorizedException('Invalid API token')
    }

    return true
  }
}
