# PolyON Module System — 설계서 v1

> 작성: Jupiter (팀장) | 2026-03-07
> 상태: 보스 리뷰 대기

---

## 1. 목표

Synology 패키지 센터처럼 — **설치 · 삭제 · 업데이트 · 의존성 자동 해결**.
Console이 예정하지 않은 3rd-party 모듈도 자기 UI를 가져와서 플러그인처럼 동작.

### 핵심 요구사항

| # | 요구사항 |
|---|---------|
| R1 | 모듈 원클릭 설치/삭제 |
| R2 | 의존성 자동 해결 (A 설치 시 B 필요하면 B 먼저 설치) |
| R3 | 삭제 시 역의존성 체크 (B에 의존하는 A가 있으면 경고/거부) |
| R4 | 모듈 설치 시 Admin Console 메뉴 자동 등록/삭제 |
| R5 | Console이 모르는 모듈도 자기 UI 번들로 메뉴+페이지 제공 (Micro-Frontend) |
| R6 | 코어 모듈(DC, Keycloak, PG 등)은 삭제 불가 |
| R7 | 모듈별 DB/OIDC/Ingress 자동 프로비저닝 |

---

## 2. 아키텍처 개요

```
┌──────────────────────────────────────────────────────┐
│                    Admin Console                      │
│                                                      │
│  ┌─────────┐  ┌──────────────────────────────────┐   │
│  │ Shell   │  │         ModuleHost               │   │
│  │(Header, │  │  ┌────────┐ ┌────────┐ ┌──────┐  │   │
│  │ SideNav,│  │  │Mail UI │ │Chat UI │ │?? UI │  │   │
│  │ Theme)  │  │  │(bundle)│ │(bundle)│ │(3rd) │  │   │
│  │         │  │  └────────┘ └────────┘ └──────┘  │   │
│  └─────────┘  └──────────────────────────────────┘   │
│       ▲                    ▲                          │
│       │    window.__POLYON__                          │
│       │    (React, Carbon, SDK)                       │
└───────┼──────────────────┼───────────────────────────┘
        │                  │
   GET /api/modules/nav    │  GET /modules/{id}/manifest.json
        │                  │  import(/modules/{id}/entry.js)
        ▼                  ▼
┌──────────────────────────────────────────────────────┐
│                   PolyON Core API                     │
│                                                      │
│  /api/modules/catalog    — 전체 카탈로그              │
│  /api/modules/nav        — 설치된 모듈의 메뉴 정보     │
│  /api/modules/{id}/install   — 설치 요청              │
│  /api/modules/{id}/uninstall — 삭제 요청              │
│  /api/modules/{id}/status    — 상태 조회              │
└───────────────────────────┬──────────────────────────┘
                            │
                   Module Lifecycle Manager
                            │
                    ┌───────▼───────┐
                    │  Kubernetes   │
                    │  API Server   │
                    └───────────────┘
```

---

## 3. Module Manifest 표준

모듈 = 자기 자신을 완전히 설명하는 선언적 패키지.

### 3.1 `module.yaml` 스펙

```yaml
apiVersion: polyon.io/v1
kind: Module
metadata:
  id: mattermost                    # 고유 식별자 (영문 소문자, 하이픈)
  name: HELIOS Chat                 # 표시명
  version: 10.6.0                   # 시맨틱 버전
  category: engine                  # engine | ai | process | monitoring | infra
  icon: Chat                        # @carbon/icons-react 컴포넌트명
  accent: "#0058CC"                 # 테마 색상
  description: "팀 메신저 · 채널 기반 소통"

spec:
  engine: mattermost                # 기반 기술

  # ── 의존성 ──
  requires:                         # 필수 — 없으면 자동 설치 제안
    - id: postgresql
      reason: "채팅 데이터 저장"
    - id: keycloak
      reason: "SSO 인증 연동"

  optional:                         # 선택 — 있으면 연동, 없으면 무시
    - id: opensearch
      reason: "메시지 전문검색"

  # ── 보호 등급 ──
  protected: false                  # true면 삭제 불가 (코어 모듈)

  # ── K8s 리소스 ──
  resources:
    deployment:
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
              name: polyon-common-secret
              key: DATABASE_URL
      health:
        path: /api/v4/system/ping
        port: 8065
      resources:
        requests: { cpu: 100m, memory: 256Mi }
        limits: { cpu: 500m, memory: 1Gi }

    service:
      port: 8065
      targetPort: 8065

    ingress:
      subdomain: chat               # → chat.{base_domain}
      annotations: {}

    pvc:                             # 선택
      - name: data
        size: 10Gi
        mountPath: /mattermost/data

  # ── DB 프로비저닝 ──
  database:
    name: mattermost                # PostgreSQL DB 이름
    user: mattermost                # DB 유저 (자동 생성)
    migrations: auto                # auto = 엔진 자체 마이그레이션

  # ── OIDC 클라이언트 ──
  oidc:
    realm: helios                   # Keycloak realm
    clientId: mattermost
    protocol: openid-connect
    publicClient: false
    redirectUris:
      - "https://chat.{{.Domain}}/*"
    webOrigins:
      - "https://chat.{{.Domain}}"

  # ── Admin Console UI ──
  admin:
    # 사이드바 메뉴 정의
    nav:
      title: Mattermost
      section: SERVICES             # SERVICES | INFRASTRUCTURE | GOVERNANCE | SYSTEM
      icon: Chat                    # @carbon/icons-react 이름
      defaultPath: /chat
      sortOrder: 20                 # 섹션 내 정렬 순서
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

    # UI 번들 (Micro-Frontend)
    ui:
      entry: entry.js               # 메인 엔트리 파일
      pages:                         # 라우트 → 컴포넌트 매핑
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
    confirm: true                   # 사용자 확인 필수
    keepData: ask                   # ask | always | never
    databases: [mattermost]         # 삭제 대상 DB
    pvcs: [polyon-chat-data]        # 삭제 대상 PVC
    oidcClients: [mattermost]       # 삭제 대상 KC 클라이언트
```

### 3.2 카테고리별 보호 정책

| 카테고리 | 모듈 | protected | 비고 |
|---------|------|-----------|------|
| **core** | polyon-console, polyon-core, polyon-dc, keycloak | `true` | 삭제 시 시스템 붕괴 |
| **infra** | postgresql, redis | `true` | 다른 모든 모듈이 의존 |
| **infra** | opensearch, rustfs, traefik | `false` | 의존 모듈 없으면 삭제 가능 |
| **engine** | mattermost, nextcloud, odoo 등 | `false` | 자유 설치/삭제 |
| **ai** | litellm, openclaw, mem0 | `false` | 자유 설치/삭제 |
| **process** | operaton, n8n | `false` | 자유 설치/삭제 |
| **monitoring** | prometheus, grafana 등 | `false` | 자유 설치/삭제 |

---

## 4. Module Lifecycle Manager

### 4.1 설치 흐름

```
사용자: "Chat 설치" 클릭
         │
         ▼
  ① Manifest 로드 (module.yaml)
         │
         ▼
  ② 의존성 해결 (Dependency Resolution)
     ├── postgresql 필요 → 이미 active ✓
     ├── keycloak 필요 → 이미 active ✓
     └── opensearch (optional) → active ✓, 연동 활성화
         │
         ▼
  ③ 프로비저닝 파이프라인 (순서 보장)
     ├── 3-a. DB 생성: CREATE DATABASE mattermost
     ├── 3-b. DB 유저 생성 + 권한 부여
     ├── 3-c. OIDC 클라이언트 등록 (Keycloak API)
     ├── 3-d. K8s 리소스 배포:
     │        ServiceAccount → PVC → Service →
     │        Deployment → Ingress
     ├── 3-e. UI 번들 추출 (initContainer → 공유 PVC)
     └── 3-f. 헬스 체크 대기 (readiness probe)
         │
         ▼
  ④ 상태 업데이트
     ├── polyon_modules 테이블: status = "active"
     ├── polyon_module_nav 테이블: 메뉴 정보 저장
     └── Console에 이벤트 전송 (WebSocket 또는 polling)
         │
         ▼
  ⑤ Console 자동 갱신
     ├── 사이드바에 "Mattermost" 메뉴 등장
     └── /chat 라우트 동적 등록
```

### 4.2 삭제 흐름

```
사용자: "Chat 삭제" 클릭
         │
         ▼
  ① 역의존성 체크 (Reverse Dependency)
     └── mattermost에 의존하는 모듈? → 없음 ✓
         │
         ▼
  ② 확인 다이얼로그
     ├── "채팅 데이터를 보존하시겠습니까?"
     └── [보존] / [삭제] / [취소]
         │
         ▼
  ③ 역프로비저닝 (역순)
     ├── 3-a. K8s 리소스 삭제: Ingress → Deployment → Service → PVC(선택)
     ├── 3-b. OIDC 클라이언트 삭제
     └── 3-c. DB 삭제(선택) 또는 보존
         │
         ▼
  ④ 상태 업데이트
     ├── polyon_modules: status = "planned"
     ├── polyon_module_nav: 레코드 삭제
     └── UI 번들 정리
         │
         ▼
  ⑤ Console 사이드바에서 메뉴 제거
```

### 4.3 의존성 해결 알고리즘

```
function resolve(moduleId, action):
  manifest = loadManifest(moduleId)

  if action == INSTALL:
    for dep in manifest.requires:
      if dep.status != "active":
        # 재귀적으로 의존성의 의존성도 해결
        plan.add(INSTALL, dep.id)
        resolve(dep.id, INSTALL)
    plan.add(INSTALL, moduleId)

  if action == UNINSTALL:
    # 이 모듈에 의존하는 모든 모듈 검색
    dependents = findModulesDependingOn(moduleId)
    if dependents.length > 0:
      if moduleId.protected:
        REJECT("코어 모듈은 삭제할 수 없습니다")
      else:
        WARN("다음 모듈이 영향받습니다: {dependents}")
        # 사용자 확인 후 cascade 삭제 또는 거부
    plan.add(UNINSTALL, moduleId)

  return plan  # 순서가 보장된 실행 계획
```

---

## 5. Micro-Frontend 설계

### 5.1 공유 라이브러리 (Console이 노출)

Console이 빌드 시점에 `window.__POLYON__`에 공유 라이브러리를 노출합니다.
모듈은 이것을 사용하여 **동일한 React/Carbon 인스턴스**를 공유합니다.

```typescript
// Console 초기화 시 (App.tsx 또는 index.tsx)
import React from 'react';
import ReactDOM from 'react-dom';
import * as CarbonReact from '@carbon/react';
import * as CarbonIcons from '@carbon/icons-react';
import * as ReactRouterDOM from 'react-router-dom';
import { apiFetch } from './api/client';
import { useAuth } from './auth/useAuth';
import { PageHeader } from './components/PageHeader';

window.__POLYON__ = {
  // 프레임워크
  React,
  ReactDOM,
  ReactRouterDOM,

  // Carbon Design
  CarbonReact,       // Tile, Tag, DataTable, Button, ...
  CarbonIcons,       // Email, Chat, Settings, ...

  // PolyON SDK
  sdk: {
    apiFetch,        // 인증 토큰 포함된 API 호출
    useAuth,         // 인증 상태 훅
    PageHeader,      // 공통 페이지 헤더
    // 향후 추가: useToast, useModule, useConfig
  },

  // 버전 정보 (호환성 체크용)
  version: {
    sdk: '1.0.0',
    react: React.version,
    carbon: '1.71.0',
  },
};
```

### 5.2 모듈 UI 번들 구조

```
polyon-chat-ui/
├── manifest.json          # 메뉴 + 라우트 + 메타 정보
├── entry.js               # 메인 엔트리 (default export)
├── pages/
│   ├── ChatPage.js        # 개별 페이지 컴포넌트
│   ├── ChatTeamsPage.js
│   ├── ChatChannelsPage.js
│   └── ChatSettingsPage.js
└── assets/                # 모듈 전용 정적 파일 (선택)
    └── chat-icon.svg
```

### 5.3 모듈 entry.js 규약

```javascript
// entry.js — 모듈의 진입점
// Console이 동적으로 import하는 파일

const { React, CarbonReact, sdk } = window.__POLYON__;

// 페이지 컴포넌트 (lazy load)
export const pages = {
  ChatPage:         () => import('./pages/ChatPage.js'),
  ChatTeamsPage:    () => import('./pages/ChatTeamsPage.js'),
  ChatChannelsPage: () => import('./pages/ChatChannelsPage.js'),
  ChatUsersPage:    () => import('./pages/ChatUsersPage.js'),
  ChatSettingsPage: () => import('./pages/ChatSettingsPage.js'),
};

// 모듈 초기화 (선택적)
export function init(context) {
  // context.moduleId, context.config 등
  // 모듈별 전역 상태 초기화, API 클라이언트 설정 등
}

// 모듈 정리 (삭제 시, 선택적)
export function cleanup() {
  // 이벤트 리스너 해제, 타이머 정리 등
}
```

### 5.4 모듈 페이지 작성 규약

```javascript
// pages/ChatPage.js
// 모듈 개발자가 작성하는 페이지 — Console 내장 페이지와 동일한 패턴

const { React, CarbonReact, CarbonIcons, sdk } = window.__POLYON__;
const { useState, useEffect } = React;
const { DataTable, TableContainer, Table, TableHead, TableRow,
        TableHeader, TableBody, TableCell, Tag, Button } = CarbonReact;
const { Chat, Add } = CarbonIcons;
const { apiFetch, PageHeader } = sdk;

export default function ChatPage() {
  const [channels, setChannels] = useState([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    apiFetch('/modules/mattermost/api/channels')
      .then(setChannels)
      .finally(() => setLoading(false));
  }, []);

  return (
    <>
      <PageHeader
        title="Chat"
        description="팀 메신저 관리"
        actions={<Button renderIcon={Add}>채널 생성</Button>}
      />
      {/* Carbon DataTable 등 자유롭게 사용 */}
    </>
  );
}
```

### 5.5 ModuleHost 컴포넌트 (Console 측)

```tsx
// Console: components/ModuleHost.tsx
// 모듈 UI를 동적으로 로드하여 마운트하는 컨테이너

interface ModuleHostProps {
  moduleId: string;
  pageName: string;
}

function ModuleHost({ moduleId, pageName }: ModuleHostProps) {
  const [PageComponent, setPage] = useState<React.ComponentType | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        // 1. manifest.json 가져오기
        const manifest = await fetch(
          `/modules/${moduleId}/manifest.json`
        ).then(r => r.json());

        // 2. entry.js 동적 로드
        const entry = await import(
          /* webpackIgnore: true */
          `/modules/${moduleId}/${manifest.entry}`
        );

        // 3. 모듈 초기화 (선택)
        if (entry.init) {
          await entry.init({ moduleId, config: manifest.config });
        }

        // 4. 페이지 컴포넌트 로드
        const loader = entry.pages[pageName];
        if (!loader) throw new Error(`Page not found: ${pageName}`);

        const mod = await loader();
        if (!cancelled) {
          setPage(() => mod.default);
          setLoading(false);
        }
      } catch (e) {
        if (!cancelled) {
          setError(e.message);
          setLoading(false);
        }
      }
    })();
    return () => { cancelled = true; };
  }, [moduleId, pageName]);

  if (loading) return <SkeletonText paragraph lines={6} />;
  if (error) return (
    <InlineNotification
      kind="error"
      title="모듈 로드 실패"
      subtitle={`${moduleId}: ${error}`}
    />
  );
  if (!PageComponent) return null;

  // ErrorBoundary로 감싸서 모듈 크래시가 Console을 죽이지 않도록
  return (
    <ModuleErrorBoundary moduleId={moduleId}>
      <PageComponent />
    </ModuleErrorBoundary>
  );
}
```

### 5.6 동적 라우트 등록 (Console App.tsx)

```tsx
// App.tsx — 하드코딩 라우트 + 동적 모듈 라우트 병합

function App() {
  return (
    <HashRouter>
      <Routes>
        {/* Setup Wizard */}
        <Route path="/setup" element={<SetupLayout />}>
          <Route index element={<SetupPage />} />
        </Route>

        {/* Console */}
        <Route element={<ConsoleLayout />}>
          {/* 코어 라우트 — 항상 존재 (Console 빌드에 포함) */}
          <Route path="/" element={<DashboardPage />} />
          <Route path="/users" element={<UsersPage />} />
          <Route path="/settings/*" element={<SettingsRoutes />} />
          {/* ... 코어 라우트 ... */}

          {/* 모듈 라우트 — 동적 생성 */}
          <Route path="/*" element={<DynamicModuleRouter />} />
        </Route>
      </Routes>
    </HashRouter>
  );
}

// 설치된 모듈의 라우트를 동적으로 매칭
function DynamicModuleRouter() {
  const location = useLocation();
  const { installedModules } = useModuleStore();

  for (const mod of installedModules) {
    for (const route of mod.routes) {
      if (matchPath(route.path, location.pathname)) {
        return <ModuleHost moduleId={mod.id} pageName={route.component} />;
      }
    }
  }

  return <Navigate to="/" replace />;
}
```

### 5.7 동적 사이드바 (Console ConsoleLayout.tsx)

```tsx
// ConsoleLayout.tsx — 하드코딩 MODULES 제거, API에서 로드

function ConsoleLayout() {
  const { coreModules, dynamicModules, loading } = useModuleNav();

  // coreModules: 항상 표시 (Dashboard, Directory, Settings 등)
  // dynamicModules: API에서 로드 (설치된 모듈만)

  return (
    <nav>
      {/* 코어 메뉴 */}
      {coreModules.map(mod => <NavItem key={mod.id} {...mod} />)}

      {/* 동적 모듈 메뉴 — 섹션별 그룹 */}
      {Object.entries(groupBySection(dynamicModules)).map(([section, mods]) => (
        <div key={section}>
          <div className="he-nav__section">{section}</div>
          {mods.map(mod => <NavItem key={mod.id} {...mod} />)}
        </div>
      ))}
    </nav>
  );
}

// API 호출 훅
function useModuleNav() {
  const [modules, setModules] = useState({ core: [], dynamic: [] });

  useEffect(() => {
    apiFetch('/api/modules/nav').then(data => {
      setModules({
        core: data.modules.filter(m => m.protected),
        dynamic: data.modules.filter(m => !m.protected),
      });
    });
  }, []);

  return { coreModules: modules.core, dynamicModules: modules.dynamic };
}
```

---

## 6. UI 번들 서빙

### 6.1 번들 저장 및 배포

```
모듈 이미지 빌드 시:
  docker image:
  ├── /app                    ← 백엔드 (mattermost 서버)
  └── /module-ui/             ← 프론트엔드 번들
      ├── manifest.json
      ├── entry.js
      └── pages/

모듈 설치 시 (Lifecycle Manager):
  ① initContainer가 이미지의 /module-ui/ →
     공유 PVC의 /modules/{moduleId}/ 에 복사

  ② Console의 nginx가 /modules/ 경로 서빙:
     location /modules/ {
       alias /modules/;
       add_header Cache-Control "public, max-age=3600";
     }
```

### 6.2 Console nginx 설정 변경

```nginx
# 기존: 정적 파일 + SPA fallback
# 추가: 모듈 번들 서빙

server {
    listen 80;

    # Console 정적 파일
    location / {
        root /usr/share/nginx/html;
        try_files $uri $uri/ /index.html;
    }

    # 모듈 UI 번들
    location /modules/ {
        alias /modules/;                    # 공유 PVC 마운트
        add_header Cache-Control "public, max-age=3600";
        add_header Access-Control-Allow-Origin *;
    }

    # Core API 프록시
    location /api/ {
        proxy_pass http://polyon-core:8000/;
    }
}
```

### 6.3 K8s 공유 볼륨 구조

```yaml
# 모듈 UI 번들 공유 PVC
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: polyon-module-ui
  namespace: polyon
spec:
  accessModes: [ReadWriteMany]     # 여러 Pod에서 읽기
  resources:
    requests:
      storage: 1Gi

# Console Deployment에 마운트
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

## 7. DB 스키마

### 7.1 polyon_modules 테이블

```sql
CREATE TABLE polyon_modules (
  id               TEXT PRIMARY KEY,            -- module manifest id
  name             TEXT NOT NULL,
  description      TEXT NOT NULL DEFAULT '',
  category         TEXT NOT NULL,               -- engine|ai|process|monitoring|infra
  version          TEXT NOT NULL DEFAULT '',
  engine           TEXT NOT NULL DEFAULT '',
  icon             TEXT NOT NULL DEFAULT '',
  accent           TEXT NOT NULL DEFAULT '#393939',

  -- 상태
  status           TEXT NOT NULL DEFAULT 'available',
                   -- available | installing | active | error | uninstalling
  protected        BOOLEAN NOT NULL DEFAULT false,

  -- 의존성 (JSON)
  requires         JSONB NOT NULL DEFAULT '[]',
  optional_deps    JSONB NOT NULL DEFAULT '[]',

  -- 리소스 정보
  manifest         JSONB NOT NULL DEFAULT '{}', -- 전체 module.yaml 원본
  installed_at     TIMESTAMPTZ,
  updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

### 7.2 polyon_module_nav 테이블

```sql
CREATE TABLE polyon_module_nav (
  module_id        TEXT PRIMARY KEY REFERENCES polyon_modules(id) ON DELETE CASCADE,
  title            TEXT NOT NULL,
  section          TEXT NOT NULL DEFAULT 'SERVICES',
  icon             TEXT NOT NULL DEFAULT '',
  default_path     TEXT NOT NULL,
  sort_order       INTEGER NOT NULL DEFAULT 50,
  nav_items        JSONB NOT NULL DEFAULT '[]',  -- 서브메뉴 아이템
  routes           JSONB NOT NULL DEFAULT '[]',  -- path → component 매핑
  created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

### 7.3 polyon_module_events 테이블

```sql
CREATE TABLE polyon_module_events (
  id               SERIAL PRIMARY KEY,
  module_id        TEXT NOT NULL,
  event_type       TEXT NOT NULL,                -- install | uninstall | upgrade | error
  status           TEXT NOT NULL,                -- started | completed | failed
  message          TEXT,
  details          JSONB DEFAULT '{}',
  created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

---

## 8. API 설계

### 8.1 Module Catalog API

```
GET /api/modules/catalog
Response:
{
  "modules": [
    {
      "id": "mattermost",
      "name": "HELIOS Chat",
      "description": "팀 메신저 · 채널 기반 소통",
      "category": "engine",
      "version": "10.6.0",
      "icon": "Chat",
      "accent": "#0058CC",
      "status": "available",      // available | active | installing
      "protected": false,
      "requires": ["postgresql", "keycloak"],
      "optional": ["opensearch"]
    },
    ...
  ],
  "categories": ["engine", "ai", "process", "monitoring", "infra"]
}
```

### 8.2 Module Nav API (Console 메뉴용)

```
GET /api/modules/nav
Response:
{
  "modules": [
    {
      "id": "mail",
      "title": "Mail",
      "section": "SERVICES",
      "icon": "Email",
      "defaultPath": "/mail",
      "sortOrder": 10,
      "protected": true,
      "items": [
        { "label": "개요", "path": "/mail", "icon": "Email" },
        { "type": "divider" },
        ...
      ],
      "routes": [
        { "path": "/mail", "component": "MailPage" },
        ...
      ],
      "uiBundled": true           // Console에 내장된 모듈인가?
    },
    ...
  ]
}
```

### 8.3 Module Install/Uninstall API

```
POST /api/modules/{id}/install
Request: { "config": { ... } }      // 선택적 설정 오버라이드
Response:
{
  "status": "installing",
  "plan": [
    { "step": 1, "action": "check_deps", "status": "completed" },
    { "step": 2, "action": "create_database", "status": "pending" },
    { "step": 3, "action": "register_oidc", "status": "pending" },
    { "step": 4, "action": "deploy_k8s", "status": "pending" },
    { "step": 5, "action": "extract_ui", "status": "pending" },
    { "step": 6, "action": "health_check", "status": "pending" }
  ],
  "eventId": 42                      // 진행 상태 polling용
}

POST /api/modules/{id}/uninstall
Request: { "keepData": true }
Response:
{
  "status": "uninstalling",
  "plan": [...],
  "eventId": 43
}

GET /api/modules/{id}/install/status?eventId=42
Response:
{
  "status": "installing",            // installing | completed | failed
  "currentStep": 3,
  "totalSteps": 6,
  "plan": [
    { "step": 1, "action": "check_deps", "status": "completed" },
    { "step": 2, "action": "create_database", "status": "completed" },
    { "step": 3, "action": "register_oidc", "status": "running" },
    ...
  ]
}
```

---

## 9. 코어 메뉴 vs 동적 메뉴

### 항상 표시되는 코어 메뉴 (Console 빌드에 포함)

| 메뉴 | serviceId | 비고 |
|------|-----------|------|
| Dashboard | home | 항상 |
| Applications | apps | 항상 |
| Directory | directory | DC 의존 (항상 설치) |
| Directory Tree | tree-view | DC 의존 (항상 설치) |
| Networking | networking | DC 의존 (항상 설치) |
| Containers | containers | 항상 |
| Database | database | 항상 |
| Security | security | 항상 |
| Settings | settings | 항상 |

### 동적 메뉴 (모듈 설치 시에만 표시)

| 메뉴 | moduleId | 비고 |
|------|----------|------|
| Mail | stalwart | 기초 설치에 포함, 삭제 가능 |
| Mattermost | mattermost | 선택 설치 |
| AI Platform | litellm | 선택 설치 |
| Automation | n8n | 선택 설치 |
| BPMN | operaton | 선택 설치 |
| Monitoring | prometheus | 선택 설치 |

---

## 10. 모듈 빌드 가이드라인

### 10.1 모듈 UI 빌드 스크립트

```bash
#!/bin/bash
# build-module-ui.sh — 모듈 UI 번들 빌드

MODULE_ID=$1
UI_DIR="modules/$MODULE_ID/ui"

# 1. 빌드 (Vite/esbuild 등)
cd $UI_DIR
npm run build

# 2. 번들 출력 → dist/
#    dist/
#    ├── manifest.json
#    ├── entry.js
#    └── pages/

# 3. Docker 이미지에 포함
#    COPY dist/ /module-ui/
```

### 10.2 모듈 개발 규칙

| # | 규칙 |
|---|------|
| 1 | `window.__POLYON__`의 React/Carbon만 사용 (중복 번들링 금지) |
| 2 | 모듈 전용 npm 패키지는 번들에 포함 가능 (예: chart.js) |
| 3 | 전역 CSS 오염 금지 — CSS Modules 또는 scoped styles |
| 4 | Carbon Design 컴포넌트/패턴 준수 (제4원칙) |
| 5 | API 호출은 반드시 `sdk.apiFetch` 사용 (인증 토큰 자동) |
| 6 | 에러 발생 시 Console을 죽이지 않도록 자체 try-catch |

---

## 11. 구현 순서 (Phase 1~4)

| Phase | 작업 | 산출물 |
|-------|------|--------|
| **1-1** | DB 스키마 (polyon_modules, _nav, _events) | Go migration |
| **1-2** | Module Catalog/Nav API | GET /modules/catalog, /modules/nav |
| **1-3** | `window.__POLYON__` 노출 | Console index.tsx 수정 |
| **1-4** | ModuleHost + ErrorBoundary 컴포넌트 | Console components/ |
| **1-5** | ConsoleLayout 동적 메뉴 전환 | 하드코딩 MODULES 제거 → API 기반 |
| **2-1** | Mail 모듈 분리 (PoC) | 내장 Mail 페이지 → 모듈 번들 |
| **2-2** | 동적 라우트 등록 | DynamicModuleRouter |
| **2-3** | 모듈 UI PVC + nginx 설정 | K8s manifest |
| **3-1** | Module Lifecycle Manager | Install/Uninstall API |
| **3-2** | 의존성 해결 엔진 | resolve() 알고리즘 |
| **3-3** | DB/OIDC/Ingress 프로비저닝 | 자동 프로비저닝 파이프라인 |
| **4-1** | Module Store UI | Synology 스타일 화면 |
| **4-2** | 나머지 모듈 manifest 작성 | Chat, Wiki, ERP 등 |

---

## 12. 보스 결정 필요 사항

| # | 항목 | 선택지 | 추천 |
|---|------|--------|------|
| D1 | 모듈 manifest 저장 위치 | A) Core에 embed / B) 별도 Git repo / C) Operator에 | **A** — Core가 카탈로그 관장 |
| D2 | UI 번들 서빙 방식 | A) Console nginx 공유 PVC / B) Core가 직접 serve | **A** — 정적 파일은 nginx |
| D3 | 기존 내장 모듈(Mail 등) 분리 시점 | A) Phase 2에서 즉시 / B) 새 모듈부터 적용, 기존은 나중에 | **B** — 리스크 최소화 |
| D4 | 3rd-party 모듈 등록 방식 | A) Docker Hub label 기반 / B) 별도 모듈 레지스트리 | **B** — 장기 (Phase 5+) |
| D5 | 모듈 간 통신 | A) Core API 경유만 / B) 모듈 간 직접 통신 허용 | **A** — Core가 관문 |

---

*이 설계서를 기반으로 보스 승인 후 Phase 1 구현에 착수합니다.*
