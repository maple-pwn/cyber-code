import type { FindingState, ImmutableEvidence, ScopeSnapshot } from '@cyber/protocol';

export const LAB_SCOPE: ScopeSnapshot = {
  targets: ['juice-shop.lab'],
  allowedActions: ['passive-recon', 'route-enumeration', 'bounded-login-verification'],
  deniedActions: ['destructive', 'persistence', 'credential-stuffing'],
  riskCeiling: 'medium',
};

export const RECON_EVIDENCE: readonly ImmutableEvidence[] = [
  { id: 'evidence-runtime', kind: 'technology', summary: 'Node.js runtime identified', data: { runtime: 'Node.js' } },
  { id: 'evidence-framework', kind: 'technology', summary: 'Express framework identified', data: { framework: 'Express' } },
  { id: 'evidence-route-count', kind: 'route-inventory', summary: '24 API routes enumerated', data: { count: 24 } },
  { id: 'evidence-login-route', kind: 'route', summary: 'Login API route observed', data: { method: 'POST', path: '/rest/user/login' } },
  { id: 'evidence-product-route', kind: 'route', summary: 'Product API route observed', data: { method: 'GET', path: '/api/Products' } },
  { id: 'evidence-security-headers', kind: 'headers', summary: 'Security header baseline recorded', data: { server: 'Express' } },
  { id: 'evidence-auth-shape', kind: 'schema', summary: 'Authentication request shape recorded', data: { fields: ['email', 'password'] } },
  { id: 'evidence-scope', kind: 'scope', summary: 'Evidence collected within juice-shop.lab', data: { target: 'juice-shop.lab' } },
];

export const CANDIDATE_FINDING: FindingState = {
  id: 'finding-login-injection',
  title: 'Potential login injection',
  severity: 'high',
  status: 'candidate',
  confidence: 'medium',
  evidenceIds: ['evidence-login-route', 'evidence-auth-shape'],
};

export const VERIFIED_EVIDENCE: ImmutableEvidence = {
  id: 'evidence-bounded-verification',
  kind: 'verification',
  summary: 'Bounded login verification confirmed impact',
  data: { target: 'juice-shop.lab', attempts: 1, impact: 'authentication bypass' },
};
