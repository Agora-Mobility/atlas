Scalar.createApiReference('#app', {
  url: '/docs/openapi.json',
  proxyUrl: '',
  withDefaultFonts: false,
  telemetry: false,
  persistAuth: false,
  showDeveloperTools: 'never',
  agent: { disabled: true },
  mcp: { disabled: true },
  authentication: {
    preferredSecurityScheme: [['ClientId', 'BearerAuth'], 'ClientId'],
    securitySchemes: {
      ClientId: { value: 'agora-customer-app' },
      BearerAuth: { value: '' },
    },
  },
})
