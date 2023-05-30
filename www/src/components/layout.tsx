import { useAtom } from 'jotai'
import { usernameAtom } from '@/store/userState'
import React, { useState } from 'react'
import { Layout as SemiLayout } from '@douyinfe/semi-ui'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'

const queryClient = new QueryClient()
const { Header, Footer, Sider, Content } = SemiLayout

export const Layout = ({
  header,
  side,
  main,
}: {
  header: React.ReactNode
  side: React.ReactNode
  main: React.ReactNode
}) => {
  return (
    <QueryClientProvider client={queryClient}>
      <SemiLayout>
        <Header>{header}</Header>
        <SemiLayout>
          <Sider>{side}</Sider>
          <Content>{main}</Content>
        </SemiLayout>
      </SemiLayout>
    </QueryClientProvider>
  )
}
