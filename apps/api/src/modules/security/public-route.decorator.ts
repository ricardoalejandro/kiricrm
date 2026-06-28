import { SetMetadata } from '@nestjs/common'

export const PUBLIC_ROUTE_METADATA = 'kiri:public-route'

export const PublicRoute = () => SetMetadata(PUBLIC_ROUTE_METADATA, true)
