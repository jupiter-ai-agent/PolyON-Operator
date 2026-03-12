# PolyON Module System — 설계서 v2

> 작성: Jupiter (팀장) | 2026-03-07
> v1 → v2: 보스 리뷰 반영, 결정사항 확정
> 상태: **확정**

---

## 1. 목표

Synology 패키지 센터처럼 — **설치 · 삭제 · 업데이트 · 의존성 자동 해결**.
PP(PolyON Platform) 규격만 맞추면 **누구든** 모듈을 만들어 추가할 수 있다.

---

## 2. 핵심 원칙 (보스 확정)

| # | 원칙 | 내용 |
|---|------|------|
| **P1** | 모듈 자기완결 | 모듈 이미지가 자기 자신의 모든 정보를 가진다 (manifest, UI, 메뉴, 의존성) |
| **P2** | Foundation 불가침 | PG, Redis, OpenSearch, RustFS, Traefik, DC, Keycloak, Mail, Core, Console은 플랫폼 자체. 삭제 불가 |
| **P3** | 깨끗한 삭제 | 모듈 삭제 시 모든 흔적(DB 레코드, UI 번들, 메뉴, OIDC 클라이언트) 제거 |
| **P4** | Core는 발견자 | Core가 모듈 정보를 내장하지 않는다. 이미지에서 추출하여 발견한다 |
| **P5** | Core 관문 | 모듈 간 통신은 Core API를 경유한다 (제3원칙 — 통제와 감사) |

---

## 3. 아키텍처

### 3.1 2-Layer 구조

```
┌─────────────────────────────────────────────┐
│          FOUNDATION LAYER                    │
│          (= PolyON Platform, 삭제 불가)       │
│                                             │
│  인프라: PostgreSQL · Redis · OpenSearch     │
│         RustFS · Traefik                    │
│  인증:  Samba DC · Keycloak                 │
│  서비스: Stalwart Mail                       │
│  앱:    Core · Console                      │
│                                             │
│  설치: Operator Setup Wizard                │
│  관리: Console "시스템 정보" (상태 표시만)     │
└──────────────────┬──────────────────────────┘
                   │
                   │  Foundation 위에서 동작
                   ▼
┌─────────────────────────────────────────────┐
│          MODULE LAYER                        │
│          (사용자 선택, 설치/삭제 가능)          │
│                                             │
│  Chat · Wiki · ERP · Git · AI · BPMN       │
│  Automation · Monitoring · CMS · ...        │
│  3rd-party 모듈 (PP 규격 준수)               │
│                                             │
│  설치: Console Module Store                  │
│  관리: Module Lifecycle Manager              │
└─────────────────────────────────────────────┘
```

### 3.2 시스템 흐름

```
┌──────────────────────────────────────────────────────┐
│                    Admin Console                      │
│                                                      │
│  ┌────────────┐  ┌───────────────────────────────┐   │
│  │  Shell     │  │  Foundation Pages (내장)       │   │
│  │  (Header,  │  │  Mail, Directory, Settings... │   │
│  │  SideNav,  │  ├───────────────────────────────┤   │
│  │  Theme)    │  │  ModuleHost (동적 로딩)        │   │
│  │            │  │  Chat UI, Wiki UI, ERP UI...  │   │
│  │            │  │  (모듈 이미지에서 추출한 번들)   │   │
│  └────────────┘  └───────────────────────────────┘   │
│       ▲                    ▲                          │
│       │         window.__POLYON__                     │
│       │         (React, Carbon, SDK)                  │
└───────┼──────────────────┼───────────────────────────┘
        │                  │
   GET /api/modules/nav    │ GET /modules/{id}/manifest.json
        │                  │ import(/modules/{id}/entry.js)
        ▼                  ▼
┌──────────────────────────────────────────────────────┐
│                   PolyON Core API                     │
│                                                      │
│  Module Discovery:                                    │
│    이미지 pull → /polyon-module/module.yaml 추출      │
│    → PP 규격 검증 → DB 등록                           │
│                                                      │
│  Module Lifecycle:                                    │
│    의존성 해결 → DB 프로비저닝 → OIDC 등록            │
│    → K8s 배포 → UI 추출 → 메뉴 등록                  │
│                                                      │
│  모듈 간 통신: Core API 경유 (관문)                    │
└───────────────────────────┬──────────────────────────┘
                            │ client-go
                    ┌───────▼───────┐
                    │  Kubernetes   │
                    │  API Server   │
                    └───────────────┘
```

---

## 4. PP (PolyON Platform) 모듈 규격

### 4.1 이미지 구조

```
Docker Image: jupitertriangles/polyon-chat:v1.0.0
│
├── /app                        ← 백엔드 (모듈 자체 서비스)
│
└── /polyon-module/             ← PP 규격 디렉토리 (이름 고정)
    ├── module.yaml             ← 매니페스트 (필수)
    ├── icon.svg                ← Module Store 아이콘 (선택)
    └── ui/                     ← Admin Console UI 번들 (선택)
        ├── manifest.json
        ├── entry.js
        └── pages/
            ├── ChatPage.js
            └── ...
```

**PP 인식 규칙:** `/polyon-module/module.yaml` 파일이 존재하면 PolyON 모듈로 인식.

### 4.2 module.yaml 전체 스펙

```yaml
# ── PP Module Manifest Spec v1 ──
apiVersion: polyon.io/v1
kind: Module

# ── 기본 정보 ──
metadata:
  id: mattermost                    # 고유 식별자 (영문 소문자, 하이픈)
  name: HELIOS Chat                 # 표시명
  version: 10.6.0                   # 시맨틱 버전
  category: engine                  # engine | ai | process | monitoring
  icon: Chat                        # @carbon/icons-react 컴포넌트명
  accent: "#0058CC"                 # 테마 색상
  description: "팀 메신저 · 채널 기반 소통"
  vendor: Triangle.s                # 제작사 (선택)
  license: MIT                      # 라이선스 (선택)

spec:
  engine: mattermost                # 기반 기술명

  # ── 의존성 ──
  requires:                         # 필수 — 없으면 설치 거부 (Foundation은 항상 존재)
    - id: postgresql
      reason: "채팅 데이터 저장"
    - id: keycloak
      reason: "SSO 인증 연동"

  optional:                         # 선택 — 있으면 연동, 없으면 무시
    - id: opensearch
      feature: "메시지 전문검색"

  # ── K8s 리소스 ──
  resources:
    statefulset: false              # true면 StatefulSet, false면 Deployment (기본: false)

    image: jupitertriangles/polyon-chat:v1.0.0
    replicas: 1
    
    ports:
      - name: http
        containerPort: 8065

    env:
      - name: MM_SQLSETTINGS_DRIVERNAME
        value: postgres
      - name: MM_SQLSETTINGS_DATASOURCE
        valueFrom:
          secretKeyRef:
            name: polyon-module-mattermost
            key: DATABASE_URL

    health:
      path: /api/v4/system/ping
      port: 8065
      initialDelay: 30
      period: 10

    resources:
      requests: { cpu: 100m, memory: 256Mi }
      limits: { cpu: 500m, memory: 1Gi }

    pvc:                             # 영구 데이터 (선택)
      - name: data
        size: 10Gi
        mountPath: /mattermost/data

    configMaps: []                   # 추가 ConfigMap (선택)

  # ── 네트워크 ──
  ingress:
    subdomain: chat                  # → chat.{base_domain}
    port: 8065
    annotations: {}                  # 추가 Ingress 어노테이션 (선택)

  # ── DB 프로비저닝 ──
  database:
    create: true                     # true면 자동 CREATE DATABASE
    name: mattermost                 # DB 이름
    user: mattermost                 # DB 유저 (자동 생성)
    migrations: auto                 # auto = 엔진 자체 마이그레이션

  # ── OIDC 클라이언트 ──
  oidc:
    create: true                     # true면 Keycloak에 자동 등록
    realm: helios
    clientId: mattermost
    publicClient: false
    redirectUris:
      - "https://chat.{{.Domain}}/*"
    webOrigins:
      - "https://chat.{{.Domain}}"

  # ── Admin Console UI ──
  admin:
    nav:
      title: Mattermost
      section: SERVICES              # SERVICES | INFRASTRUCTURE | GOVERNANCE | SYSTEM
      icon: Chat                     # @carbon/icons-react 이름
      defaultPath: /chat
      sortOrder: 20                  # 섹션 내 정렬 순서
      items:
        - label: 개요
          path: /chat
          icon: Chat
        - type: divider
        - label: 팀 관리
          path: /chat/teams
          icon: UserMultiple
        - label: 채널 관리
          path: /chat/channels
          icon: Chat
        - label: 사용자 관리
          path: /chat/users
          icon: User
        - type: divider
        - label: 서버 설정
          path: /chat/settings
          icon: Settings

    ui:
      entry: entry.js
      pages:
        - path: /chat
          component: ChatPage
        - path: /chat/teams
          component: ChatTeamsPage
        - path: /chat/channels
          component: ChatChannelsPage
        - path: /chat/users
          component: ChatUsersPage
        - path: /chat/settings
          component: ChatSettingsPage

  # ── 삭제 정책 ──
  uninstall:
    confirm: true                    # 삭제 확인 다이얼로그
    dataPolicy: ask                  # ask | keepAll | deleteAll
    resources:                       # 삭제 대상 리스트
      databases: [mattermost]
      pvcs: [polyon-mattermost-data]
      oidcClients: [mattermost]
      secrets: [polyon-module-mattermost]
```

### 4.3 PP 규격 검증 (Core가 수행)

```
module.yaml 검증 항목:
├── apiVersion: polyon.io/v1 필수
├── kind: Module 필수
├── metadata.id: 영문 소문자 + 하이픈, 고유
├── metadata.name: 비어있지 않음
├── metadata.version: semver 형식
├── metadata.category: 허용값 목록 내
├── spec.resources.image: 비어있지 않음
├── spec.resources.ports: 최소 1개
├── spec.resources.health: path + port 필수
├── spec.admin.nav.title: 비어있지 않음
├── spec.admin.nav.defaultPath: / 시작
└── 기존 설치된 모듈과 id 중복 없음
```

---

## 5. Module Lifecycle Manager

### 5.1 설치 흐름

```
사용자: Module Store에서 "Chat 설치" 또는 이미지 URL 입력
         │
         ▼
  ① 이미지 Pull & Manifest 추출
     docker pull jupitertriangles/polyon-chat:v1.0.0
     → /polyon-module/module.yaml 추출
     → PP 규격 검증
         │
         ▼
  ② 의존성 해결 (Dependency Resolution)
     ├── postgresql → Foundation ✓ (항상 존재)
     ├── keycloak → Foundation ✓ (항상 존재)
     └── opensearch (optional) → Foundation ✓, 연동 활성화
         │
         ▼
  ③ 설치 계획 확인
     "다음 리소스가 생성됩니다:"
     ├── Database: mattermost
     ├── OIDC Client: mattermost
     ├── Deployment: polyon-mattermost
     ├── Service: polyon-mattermost
     ├── Ingress: chat.cmars.com
     └── PVC: polyon-mattermost-data (10Gi)
     [설치] [취소]
         │
         ▼
  ④ 프로비저닝 파이프라인 (순서 보장, 프로그레스 표시)
     Step 1/6 ██░░░░ DB 생성...
     ├── 4-a. PostgreSQL: CREATE DATABASE mattermost + CREATE USER
     ├── 4-b. K8s Secret 생성 (DB 접속 정보)
     ├── 4-c. Keycloak: OIDC 클라이언트 등록
     ├── 4-d. K8s 리소스 배포:
     │        PVC → Service → Deployment → Ingress
     ├── 4-e. UI 번들 추출 → 공유 PVC (/modules/mattermost/)
     └── 4-f. Readiness 대기 (health check 통과까지)
     Step 6/6 ██████ 완료!
         │
         ▼
  ⑤ DB 등록 & Console 갱신
     ├── polyon_modules: status = "active"
     ├── polyon_module_nav: 메뉴/라우트 정보 저장
     └── Console 사이드바에 "Mattermost" 메뉴 등장
```

### 5.2 삭제 흐름

```
사용자: Module Store에서 "Chat 삭제" 클릭
         │
         ▼
  ① 역의존성 체크
     이 모듈에 의존하는 다른 모듈? → 없음 ✓
     (있으면: "다음 모듈이 영향받습니다: [X, Y]" 경고)
         │
         ▼
  ② 삭제 확인 다이얼로그
     "HELIOS Chat을 삭제합니다."
     "채팅 데이터를 보존하시겠습니까?"
     [데이터 포함 삭제] [데이터 보존] [취소]
         │
         ▼
  ③ 역프로비저닝 (역순)
     ├── 3-a. K8s: Ingress → Deployment → Service → PVC(선택)
     ├── 3-b. Keycloak: OIDC 클라이언트 삭제
     ├── 3-c. PostgreSQL: DROP DATABASE(선택) + DROP USER
     ├── 3-d. K8s Secret 삭제
     └── 3-e. UI 번들 삭제 (/modules/mattermost/ 제거)
         │
         ▼
  ④ DB 정리 & Console 갱신
     ├── polyon_modules: 레코드 삭제
     ├── polyon_module_nav: 레코드 삭제 (CASCADE)
     └── Console 사이드바에서 메뉴 제거

  → Core에 흔적 없음. 깨끗.
```

### 5.3 의존성 해결

```
function resolve(moduleId):
  manifest = extractManifest(image)

  plan = []
  for dep in manifest.spec.requires:
    if dep.id is Foundation:
      continue  // Foundation은 항상 존재
    if dep is installed and active:
      continue
    if dep is available module:
      plan += resolve(dep.id)  // 재귀
    else:
      REJECT("필수 의존성 '{dep.id}'을 찾을 수 없습니다")

  plan += { action: INSTALL, module: moduleId }
  return plan


function checkUninstall(moduleId):
  dependents = findModulesRequiring(moduleId)
  if dependents.length > 0:
    return { blocked: true, reason: dependents }
  return { blocked: false }
```

---

## 6. Micro-Frontend

### 6.1 공유 라이브러리

Console이 `window.__POLYON__`에 노출:

```typescript
window.__POLYON__ = {
  // 프레임워크
  React,
  ReactDOM,
  ReactRouterDOM,

  // Carbon Design
  CarbonReact,       // 전체 @carbon/react 컴포넌트
  CarbonIcons,       // 전체 @carbon/icons-react

  // PolyON SDK
  sdk: {
    apiFetch,        // 인증 토큰 포함된 API 호출
    useAuth,         // 인증 상태 훅
    PageHeader,      // 공통 페이지 헤더 컴포넌트
  },

  // 버전 (호환성 체크용)
  version: {
    sdk: '1.0.0',
    react: '19.x',
    carbon: '1.71.x',
  },
};
```

### 6.2 모듈 UI entry.js 규약

```javascript
// /polyon-module/ui/entry.js

// 공유 라이브러리 사용
const { React, CarbonReact, sdk } = window.__POLYON__;

// 페이지 컴포넌트 (lazy load)
export const pages = {
  ChatPage:         () => import('./pages/ChatPage.js'),
  ChatTeamsPage:    () => import('./pages/ChatTeamsPage.js'),
  ChatChannelsPage: () => import('./pages/ChatChannelsPage.js'),
  ChatUsersPage:    () => import('./pages/ChatUsersPage.js'),
  ChatSettingsPage: () => import('./pages/ChatSettingsPage.js'),
};

// 모듈 초기화 (선택)
export function init(context) {
  // context.moduleId, context.config
}

// 모듈 정리 (선택)
export function cleanup() {}
```

### 6.3 모듈 페이지 작성 예시

```javascript
// /polyon-module/ui/pages/ChatPage.js

const { React, CarbonReact, CarbonIcons, sdk } = window.__POLYON__;
const { useState, useEffect } = React;
const { DataTable, Tag, Button } = CarbonReact;
const { Chat, Add } = CarbonIcons;
const { apiFetch, PageHeader } = sdk;

export default function ChatPage() {
  const [teams, setTeams] = useState([]);

  useEffect(() => {
    apiFetch('/modules/mattermost/api/teams').then(setTeams);
  }, []);

  return (
    <>
      <PageHeader title="Chat" description="팀 메신저 관리" />
      {/* Carbon 컴포넌트로 자유롭게 구성 */}
    </>
  );
}
```

### 6.4 ModuleHost 컴포넌트 (Console 측)

```tsx
// 모듈 UI를 동적 로드하여 마운트
function ModuleHost({ moduleId, pageName }) {
  const [Page, setPage] = useState(null);
  const [error, setError] = useState('');

  useEffect(() => {
    (async () => {
      const manifest = await fetch(`/modules/${moduleId}/manifest.json`).then(r => r.json());
      const entry = await import(`/modules/${moduleId}/${manifest.entry}`);
      if (entry.init) await entry.init({ moduleId });
      const mod = await entry.pages[pageName]();
      setPage(() => mod.default);
    })().catch(e => setError(e.message));
  }, [moduleId, pageName]);

  if (error) return <InlineNotification kind="error" title="모듈 로드 실패" subtitle={error} />;
  if (!Page) return <SkeletonText paragraph lines={6} />;

  return (
    <ModuleErrorBoundary moduleId={moduleId}>
      <Page />
    </ModuleErrorBoundary>
  );
}
```

### 6.5 Console 동적 라우트

```tsx
// App.tsx
<Route element={<ConsoleLayout />}>
  {/* Foundation 라우트 — 항상 존재 (Console 빌드에 포함) */}
  <Route path="/" element={<DashboardPage />} />
  <Route path="/users" element={<UsersPage />} />
  <Route path="/mail/*" element={<MailRoutes />} />
  <Route path="/settings/*" element={<SettingsRoutes />} />
  {/* ... Foundation 라우트 ... */}

  {/* Module 라우트 — 동적 매칭 */}
  <Route path="/*" element={<DynamicModuleRouter />} />
</Route>
```

### 6.6 Console 동적 사이드바

```tsx
// ConsoleLayout.tsx
function ConsoleLayout() {
  const { coreNav, moduleNav } = useModuleNav();
  // coreNav: Foundation 메뉴 (항상 표시, Console에 하드코딩)
  // moduleNav: GET /api/modules/nav에서 로드 (설치된 모듈만)

  return (
    <nav>
      {coreNav.map(item => <NavItem {...item} />)}
      {moduleNav.map(item => <NavItem {...item} />)}
    </nav>
  );
}
```

---

## 7. UI 번들 서빙

### 7.1 설치 시 번들 추출

```
모듈 설치 Step 4-e:

  K8s Job (또는 initContainer):
    image: jupitertriangles/polyon-chat:v1.0.0
    command: cp -r /polyon-module/ui/* /modules/mattermost/
    volumeMounts:
      - name: module-ui
        mountPath: /modules
```

### 7.2 공유 PVC

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: polyon-module-ui
  namespace: polyon
spec:
  accessModes: [ReadWriteMany]
  resources:
    requests:
      storage: 1Gi
```

### 7.3 Console nginx 설정

```nginx
server {
    listen 80;

    # Console 정적 파일
    location / {
        root /usr/share/nginx/html;
        try_files $uri $uri/ /index.html;
    }

    # 모듈 UI 번들
    location /modules/ {
        alias /modules/;
        add_header Cache-Control "public, max-age=3600";
    }

    # Core API 프록시
    location /api/ {
        proxy_pass http://polyon-core:8000/;
    }
}
```

Console Deployment에 PVC 마운트:
```yaml
volumes:
  - name: module-ui
    persistentVolumeClaim:
      claimName: polyon-module-ui
volumeMounts:
  - name: module-ui
    mountPath: /modules
    readOnly: true
```

---

## 8. DB 스키마

### 8.1 polyon_modules

```sql
CREATE TABLE polyon_modules (
  id               TEXT PRIMARY KEY,
  name             TEXT NOT NULL,
  description      TEXT NOT NULL DEFAULT '',
  category         TEXT NOT NULL,
  version          TEXT NOT NULL DEFAULT '',
  engine           TEXT NOT NULL DEFAULT '',
  image            TEXT NOT NULL,                  -- Docker 이미지 전체 경로
  icon             TEXT NOT NULL DEFAULT '',
  accent           TEXT NOT NULL DEFAULT '#393939',
  status           TEXT NOT NULL DEFAULT 'available',
                   -- available | installing | active | error | uninstalling
  requires         JSONB NOT NULL DEFAULT '[]',
  optional_deps    JSONB NOT NULL DEFAULT '[]',
  manifest         JSONB NOT NULL DEFAULT '{}',    -- module.yaml 원본 (전체)
  installed_at     TIMESTAMPTZ,
  updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

### 8.2 polyon_module_nav

```sql
CREATE TABLE polyon_module_nav (
  module_id        TEXT PRIMARY KEY
                   REFERENCES polyon_modules(id) ON DELETE CASCADE,
  title            TEXT NOT NULL,
  section          TEXT NOT NULL DEFAULT 'SERVICES',
  icon             TEXT NOT NULL DEFAULT '',
  default_path     TEXT NOT NULL,
  sort_order       INTEGER NOT NULL DEFAULT 50,
  nav_items        JSONB NOT NULL DEFAULT '[]',
  routes           JSONB NOT NULL DEFAULT '[]',
  created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

### 8.3 polyon_module_events

```sql
CREATE TABLE polyon_module_events (
  id               SERIAL PRIMARY KEY,
  module_id        TEXT NOT NULL,
  event_type       TEXT NOT NULL,
                   -- install | uninstall | upgrade | error
  status           TEXT NOT NULL,
                   -- started | completed | failed
  message          TEXT,
  details          JSONB DEFAULT '{}',
  created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

---

## 9. API 설계

### 9.1 모듈 카탈로그

```
GET /api/modules/catalog
→ DB에 등록된 모든 모듈 (available + active)

Response:
{
  "modules": [
    {
      "id": "mattermost",
      "name": "HELIOS Chat",
      "description": "팀 메신저 · 채널 기반 소통",
      "category": "engine",
      "version": "10.6.0",
      "image": "jupitertriangles/polyon-chat:v1.0.0",
      "icon": "Chat",
      "accent": "#0058CC",
      "status": "available",
      "requires": [{"id":"postgresql","reason":"채팅 데이터 저장"}],
      "optional": [{"id":"opensearch","feature":"메시지 전문검색"}]
    }
  ]
}
```

### 9.2 모듈 등록 (이미지 URL로)

```
POST /api/modules/register
Request: { "image": "jupitertriangles/polyon-chat:v1.0.0" }

→ 이미지 pull → /polyon-module/module.yaml 추출 → PP 규격 검증 → DB 등록

Response:
{
  "status": "registered",
  "module": { "id": "mattermost", "name": "HELIOS Chat", ... }
}
```

### 9.3 모듈 설치

```
POST /api/modules/{id}/install
Request: { "config": {} }

Response:
{
  "status": "installing",
  "eventId": 42,
  "plan": [
    { "step": 1, "action": "create_database", "status": "pending" },
    { "step": 2, "action": "create_secret", "status": "pending" },
    { "step": 3, "action": "register_oidc", "status": "pending" },
    { "step": 4, "action": "deploy_k8s", "status": "pending" },
    { "step": 5, "action": "extract_ui", "status": "pending" },
    { "step": 6, "action": "health_check", "status": "pending" }
  ]
}

GET /api/modules/{id}/install/status?eventId=42
→ 실시간 진행 상태
```

### 9.4 모듈 삭제

```
POST /api/modules/{id}/uninstall
Request: { "dataPolicy": "delete" }  // delete | keep

Response:
{
  "status": "uninstalling",
  "eventId": 43,
  "plan": [...]
}
```

### 9.5 모듈 Nav (Console 메뉴용)

```
GET /api/modules/nav
→ 설치된(active) 모듈의 메뉴 정보만 반환

Response:
{
  "modules": [
    {
      "id": "mattermost",
      "title": "Mattermost",
      "section": "SERVICES",
      "icon": "Chat",
      "defaultPath": "/chat",
      "sortOrder": 20,
      "items": [...],
      "routes": [
        { "path": "/chat", "component": "ChatPage" },
        { "path": "/chat/teams", "component": "ChatTeamsPage" }
      ]
    }
  ]
}
```

### 9.6 모듈 API 프록시 (모듈 간 통신)

```
ANY /api/modules/{moduleId}/api/*
→ Core가 해당 모듈의 K8s Service로 프록시

예: GET /api/modules/mattermost/api/teams
→ Core → http://polyon-mattermost:8065/api/v4/teams
```

---

## 10. Console 메뉴 구조 (확정)

### Foundation 메뉴 (항상 표시, Console 빌드에 내장)

| 메뉴 | 섹션 | 비고 |
|------|------|------|
| Dashboard | — | 항상 |
| Applications | — | SSO 관리 |
| Directory | DIRECTORY | DC 의존 |
| Directory Tree | DIRECTORY | DC 의존 |
| Mail | SERVICES | **Foundation** |
| Networking | INFRASTRUCTURE | DC 의존 |
| Containers | INFRASTRUCTURE | 항상 |
| Database | INFRASTRUCTURE | 항상 |
| Security | GOVERNANCE | 항상 |
| Settings | SYSTEM | 항상 |

### Module 메뉴 (설치 시에만 표시, API에서 동적 로드)

| 메뉴 | 섹션 | moduleId |
|------|------|----------|
| Mattermost | SERVICES | mattermost |
| HELIOS Wiki | SERVICES | affine |
| HELIOS ERP | SERVICES | odoo |
| HELIOS Git | SERVICES | gitea |
| AI Platform | SERVICES | litellm |
| BPMN | SERVICES | operaton |
| Automation | SERVICES | n8n |
| Monitoring | INFRASTRUCTURE | prometheus |
| ... | ... | 3rd-party |

---

## 11. 구현 로드맵

| Phase | 작업 | 산출물 | 기간 |
|-------|------|--------|------|
| **1-1** | DB 스키마 (modules, nav, events) | Go migration | 1일 |
| **1-2** | Module Register API (이미지 → manifest 추출) | POST /modules/register | 2~3일 |
| **1-3** | Module Nav API | GET /modules/nav | 1일 |
| **1-4** | `window.__POLYON__` 노출 | Console index.tsx | 1일 |
| **1-5** | ModuleHost + ErrorBoundary | Console component | 1일 |
| **1-6** | Console 동적 메뉴 전환 | Foundation 내장 + Module API 병합 | 2일 |
| **2-1** | Module Lifecycle Manager | Install/Uninstall 파이프라인 | 1~2주 |
| **2-2** | 의존성 해결 엔진 | resolve() | 2일 |
| **2-3** | DB/OIDC/Ingress 자동 프로비저닝 | 프로비저닝 파이프라인 | 3~5일 |
| **2-4** | UI 번들 PVC + nginx 설정 | K8s manifest | 1일 |
| **3-1** | Module Store UI (Carbon Design) | Synology 스타일 화면 | 1주 |
| **3-2** | 첫 번째 모듈 manifest 작성 (Chat) | module.yaml + UI 번들 | 3일 |
| **3-3** | E2E 테스트 (Chat 설치 → 사용 → 삭제) | 통합 테스트 | 2일 |

---

## 12. 확정된 결정사항

| # | 항목 | 결정 | 비고 |
|---|------|------|------|
| D1 | Manifest 저장 | **모듈 이미지 자체가 보유 (PP 규격)** | Core는 발견자 |
| D2 | UI 번들 서빙 | **Console nginx + 공유 PVC** | D1에 따라 자동 결정 |
| D3 | 기존 모듈 분리 | **Foundation은 내장 유지, Module만 동적** | D1에 따라 자동 결정 |
| D4 | 3rd-party 등록 | **이미지 URL 등록 (PP 규격 준수)** | D1에 따라 자동 결정 |
| D5 | 모듈 간 통신 | **Core API 경유만** | 제3원칙 준수 |
| F1 | Foundation 범위 | **PG, Redis, OS, RustFS, Traefik, DC, KC, Mail, Core, Console** | 삭제 불가 |
| F2 | Mail 위치 | **Foundation** | 그룹웨어 = 메일 포함 |

---

*이 설계서를 기반으로 Phase 1 구현에 착수합니다.*
