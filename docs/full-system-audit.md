# PolyON 시스템 전수 조사 리포트

**조사 일시**: 2026-03-06 15:42 GMT+9  
**조사 목적**: 현재 PolyON 시스템의 전체 구조와 작동 방식을 코드 레벨에서 정확히 분석  
**분석 범위**: Operator, Console, Core API, K8s 리소스, 현재 문제점

---

## 1. Operator 배포 구조 분석

### 1.1 main.go - 배포 순서와 동작 흐름

**파일**: `/Users/jupiter/openclaw/PolyON/operator/main.go`

#### 핵심 구조체와 타입 정의 (라인 1-62)
```go
type SetupConfig struct {
    Namespace            string          `json:"namespace"`
    Domain               string          `json:"domain"`
    AdminPassword        string          `json:"adminPassword"`
    ConsoleAdminPassword string          `json:"consoleAdminPassword"`
    OrgName              string          `json:"orgName"`
    Phase                string          `json:"phase"` // "infra" | "services"
    Subdomains           SubdomainConfig `json:"subdomains"`
}
```

#### 배포 단계별 순서
**인프라 단계 (runInfraSetup, 라인 157-220)**:
1. **PostgreSQL** → `postgresql.yaml`, label: `app=polyon-db`, 120s timeout
2. **Redis** → `redis.yaml`, label: `app=polyon-redis`, 60s timeout  
3. **OpenSearch** → `opensearch.yaml`, label: `app=polyon-search`, 180s timeout
4. **RustFS** → `rustfs.yaml`, label: `app=polyon-rustfs`, 120s timeout

**서비스 단계 (runServicesSetup, 라인 222-320)**:
1. **Samba DC** → `samba-dc.yaml`, label: `app=polyon-dc`, 180s timeout
2. **Keycloak** → `keycloak.yaml`, label: `app=polyon-auth`, 180s timeout  
3. **Stalwart 설정** → `stalwart-config.yaml` (ConfigMap 생성)
4. **Stalwart 메일** → `stalwart.yaml`, label: `app=polyon-mail`, 120s timeout
5. **Keycloak 프로비저닝** → `runProvisioning()` 호출
6. **Core 백엔드** → `core.yaml`, label: `app=polyon-core`, 120s timeout
7. **Console** → `console.yaml`, label: `app=polyon-console`, 120s timeout
8. **Ingress** → `ingress.yaml` (label 선택기 없음)

#### 진행 상태 추적
- `SetupProgress` 구조체로 실시간 진행 상태 관리
- `/api/state`로 현재 상태 조회 가능
- `/api/logs`로 설치 로그 조회 가능

### 1.2 k8s.go - 템플릿 설정과 비밀번호 생성

**파일**: `/Users/jupiter/openclaw/PolyON/operator/k8s.go`

#### TemplateConfig 구조체 (라인 20-42)
```go
type TemplateConfig struct {
    // 사용자 입력
    Namespace             string
    Domain                string
    AdminPassword         string // 사용자 입력 (DC 관리자)
    ConsoleAdminPassword  string // Keycloak 관리자
    OrgName               string
    
    // 계산된 필드
    DomainUpper           string // 대문자 변환
    DomainNetBIOS         string // 첫 번째 부분만 추출
    ConsoleDomain         string // console.도메인
    AuthDomain            string // auth.도메인
    MailDomain            string // mail.도메인
    PortalDomain          string // portal.도메인
    DomainDC              string // DC=도메인,DC=com 형태
    
    // 서비스별 독립 비밀번호 (Operator가 자동 생성)
    PostgresPassword      string // 24자리
    DCAdminPassword       string // = 사용자 입력
    DBPassword            string // 24자리
    KCAdminPassword       string // 24자리
    StalwartAdminPassword string // 24자리
    RustFSSecretKey       string // 24자리
    
    // TLS 인증서 (자체 서명)
    TLSCertBase64         string
    TLSKeyBase64          string
}
```

#### 비밀번호 생성 로직 (라인 45-53)
- `generatePassword(24)`: 영숫자 24자리 무작위 생성
- 각 서비스별로 독립적인 비밀번호 사용
- 캐싱 메커니즘: `getOrCreateTemplateConfig()`로 두 단계 간 비밀번호 일관성 보장

#### kubectl 적용 방식 (라인 147-165)
1. `renderManifest()`: Go text/template으로 YAML 렌더링
2. `applyManifest()`: `kubectl apply -f -`로 stdin 파이프
3. `waitForReady()`: `kubectl wait --for=condition=ready pod -l <selector>`로 대기

#### TLS 인증서 생성 (라인 206-278)
- ECDSA P-256 키 사용
- 와일드카드 인증서: `*.도메인`, `도메인`
- 10년 유효기간
- `polyon-tls` Secret으로 저장

### 1.3 provisioning.go - Keycloak/LDAP 프로비저닝

**파일**: `/Users/jupiter/openclaw/PolyON/operator/provisioning.go`

#### 프로비저닝 순서 (runProvisioning, 라인 10-79)
1. **Keycloak 준비 대기**: `/realms/master` 엔드포인트 체크 (5분 timeout)
2. **관리자 토큰 획득**: `KC_BOOTSTRAP_ADMIN_PASSWORD` 사용
3. **admin realm 생성**: Console 관리용
4. **polyon realm 생성**: 일반 사용자용
5. **OIDC 클라이언트 생성**:
   - `polyon-console` → admin realm (Console용)
   - `polyon-portal` → polyon realm (Portal용)
6. **로컬 관리자 생성**: admin realm에 `admin` 사용자
7. **LDAP 페더레이션**: polyon realm만 Samba AD 연결
8. **LDAP 동기화**: `triggerFullSync` 실행
9. **Core + Console 배포**
10. **Ingress 배포**

#### LDAP 연결 설정 (createLDAPFederation, 라인 173-210)
```go
config := map[string][]string{
    "connectionUrl":          {"ldap://polyon-dc:389"},
    "bindDn":                 {"CN=Administrator,CN=Users," + tcfg.DomainDC},
    "bindCredential":         {tcfg.DCAdminPassword},
    "usersDn":                {"CN=Users," + tcfg.DomainDC},
    "vendor":                 {"ad"}, // Active Directory
    "editMode":               {"READ_ONLY"},
    "userObjectClasses":      {"user"},
    "usernameLDAPAttribute":  {"sAMAccountName"},
}
```

### 1.4 manifests/ 디렉토리 - K8s 리소스 정의

#### 공통 설정 관리
**common-config.yaml**: 비밀이 아닌 설정 (도메인, 서비스 URL)
- 26개 환경변수 정의
- 모든 서비스가 `envFrom`으로 참조
- 예: `POLYON_DOMAIN`, `POLYON_DB_HOST`, `POLYON_AUTH_URL`

**common-secret.yaml**: 서비스별 독립 비밀번호
- 15개 비밀 값 정의
- 각 서비스별로 분리된 비밀번호 사용
- 예: `POLYON_DB_PASSWORD`, `POLYON_KC_ADMIN_PASSWORD`

#### 배포 매니페스트 구성
- **namespace.yaml**: 네임스페이스 생성
- **postgresql.yaml**: DB (StatefulSet)
- **redis.yaml**: 캐시 (StatefulSet)  
- **opensearch.yaml**: 검색엔진 (StatefulSet)
- **rustfs.yaml**: 오브젝트 스토리지 (StatefulSet)
- **samba-dc.yaml**: Active Directory (StatefulSet)
- **keycloak.yaml**: SSO 인증 (Deployment)
- **stalwart.yaml**: 메일서버 (StatefulSet)
- **core.yaml**: API 백엔드 (Deployment)
- **console.yaml**: 웹 프론트엔드 (Deployment)
- **ingress.yaml**: 외부 접근 라우팅

---

## 2. Console 구조 분석

### 2.1 nginx.conf - 프록시 라우팅 구조

**파일**: `/Users/jupiter/openclaw/PolyON/console/nginx.conf`

#### 핵심 프록시 규칙
1. **SPA 폴백**: `try_files $uri $uri/ /index.html` (라인 7-10)
2. **API 프록시**: `/api/` → `http://polyon-core:8000` (라인 17-23)
3. **인증 프록시**: `/auth/` → `http://polyon-auth:8080` (라인 39-48)
4. **Operator API**: `/api/sentinel/`, `/api/setup/`, `/api/reset/` → `http://polyon-operator:1110`
5. **DB 도구들**:
   - `/pgweb/` → `polyon-pgweb:8081`
   - `/redis/` → `polyon-redis-commander:8081`  
   - `/elasticvue/` → `polyon-elasticvue:8080`
6. **ES 프록시**: `/es-proxy/` → `polyon-search:9200` (CORS 우회용)

#### ConfigMap vs 이미지 내장 이슈
- **이미지 내장**: `/Users/jupiter/openclaw/PolyON/console/nginx.conf`
- **ConfigMap**: `polyon-console-nginx` (라인 1-122에서 확인)
- **문제**: 두 파일이 **동일한 내용**이지만 **이중 관리** 상태
- **해결 필요**: ConfigMap을 통한 단일 관리로 통합

### 2.2 React 라우팅 구조

**파일**: `/Users/jupiter/openclaw/PolyON/console/src/App.tsx`

#### 주요 라우팅 구조 (라인 120-290)
```jsx
// Setup (독립 레이아웃)
<Route path="/setup" element={<SetupLayout />}>

// 메인 Console (ConsoleLayout)  
<Route element={<ConsoleLayout />}>
  <Route path="/" element={<DashboardPage />} />
  
  // Directory 관리
  <Route path="/users" element={<UsersPage />} />
  <Route path="/groups" element={<GroupsPage />} />
  
  // Mail 관리 (25개 세부 경로)
  <Route path="/mail" element={<MailPage />} />
  <Route path="/mail/accounts" element={<MailAccountsPage />} />
  // ... 23개 추가 메일 경로
  
  // Apps & Homepage
  <Route path="/apps" element={<AppsPage />} />
  <Route path="/homepage/sites" element={<HomepageSitesPage />} />
  
  // AI Platform (8개 경로)
  <Route path="/ai" element={<AIOverviewPage />} />
  
  // 기타 섹션들...
```

#### Lazy Loading 적용
- 모든 페이지 컴포넌트를 `lazy(() => import())` 형태로 로드
- `<Suspense fallback={<PageFallback />}>` 래핑
- 초기 로딩 성능 최적화

### 2.3 ConsoleLayout.tsx - 메뉴 구조와 인증

**파일**: `/Users/jupiter/openclaw/PolyON/console/src/layouts/ConsoleLayout.tsx`

#### 모듈 정의 구조 (라인 25-66)
```typescript
interface ModuleDef {
  title: string;
  defaultPath: string;
  icon: ComponentType<{ size?: number }>;
  desc?: string;
  section?: string;
  serviceId: string; // 메뉴 동적화용 서비스 식별자
  items: NavItem[] | null;
}
```

#### 메뉴 섹션 구조
1. **Dashboard** (home)
2. **Applications** (apps) 
3. **DIRECTORY** 섹션:
   - Directory (directory)
   - Directory Tree (tree-view)
4. **SERVICES** 섹션:
   - Mail (mail) - 25개 세부 메뉴
   - Mattermost (chat)
   - AI Platform (ai)
   - Automation (automation)
   - BPMN (bpmn)
5. **INFRASTRUCTURE** 섹션:
   - Networking (networking)
   - Containers (containers)
   - Database (database)
   - Monitoring (monitoring)
6. **GOVERNANCE** 섹션:
   - Security (security)
7. **SYSTEM** 섹션:
   - Settings (settings)

#### 동적 메뉴 필터링 (라인 334-340)
```typescript
// 설치되지 않은 서비스는 메뉴에서 제외
if (!installedServices.includes(mod.serviceId)) {
  return null;
}
```

### 2.4 인증 시스템 (Keycloak OIDC)

#### keycloak.ts - 설정
**파일**: `/Users/jupiter/openclaw/PolyON/console/src/auth/keycloak.ts`
```typescript
const KEYCLOAK_CONFIG = {
  url: 'https://auth.cmars.com',   // /auth prefix 제거 (KC 26.x)
  realm: 'admin',                 // Console용 admin realm
  clientId: 'polyon-console',
} as const;
```

#### useAuth.ts - 인증 흐름
**파일**: `/Users/jupiter/openclaw/PolyON/console/src/auth/useAuth.ts`

**초기화 순서 (initAuth, 라인 48-83)**:
1. **프로비저닝 상태 확인**: `/api/sentinel/state` 체크
2. **Keycloak Realm 확인**: `/auth/realms/admin` 접근성 체크  
3. **조건부 인증**:
   - 프로비저닝 미완료 OR Realm 없음 → `skipAuth = true`
   - 정상 상태 → Keycloak PKCE 인증 진행

**토큰 관리**:
- 60초마다 토큰 갱신 체크
- 30초 전에 `keycloak.updateToken()` 호출
- 갱신 실패 시 자동 로그아웃

---

## 3. Core API 구조 분석

### 3.1 main.go - 서버 시작과 구성

**파일**: `/Users/jupiter/openclaw/PolyON/core/cmd/polyon/main.go`

#### 시작 순서 (라인 15-55)
1. **Zerolog 설정**: 콘솔 출력, 타임스탬프, 호출자 정보
2. **설정 로딩**: `config.Load()` - 환경변수 + JSON 파일
3. **서버 생성**: `server.New(cfg)` - 모든 의존성 주입
4. **모니터링 시작**: 
   - 5초 후 **Health Checker** 시작 (Docker 기반 컨테이너 모니터링)
   - **Sentinel Agent** 시작 (LLM 기반 분석)
5. **HTTP 서버 시작**: `srv.Start()`
6. **Graceful Shutdown**: SIGINT/SIGTERM 대기

### 3.2 config.go - 환경변수와 설정 관리

**파일**: `/Users/jupiter/openclaw/PolyON/core/internal/config/config.go`

#### envFirst 패턴 (라인 346-352)
```go
func envFirst(keys ...string) string {
    for _, k := range keys {
        if v := os.Getenv(k); v != "" {
            return v
        }
    }
    return ""
}
```

**신규-레거시 환경변수 호환성**:
- `POLYON_DB_PASSWORD` → `DB_PASSWORD`
- `POLYON_AUTH_URL` → `KEYCLOAK_URL`
- `POLYON_STALWART_URL` → `STALWART_URL`

#### 설정 소스 우선순위 (Load, 라인 116-200)
1. **환경변수** (최우선)
2. **polyon/.env 파일** 
3. **setup.json** (도메인 정보)
4. **smtp.json** (메일 설정)
5. **기본값** (폴백)

#### DB 기반 동적 URL (ApplyInfraServices, 라인 87-126)
- 서버 시작 후 DB 연결 성공 시 `infra_services` 테이블에서 URL 재설정
- 환경변수 < DB 설정 우선순위

### 3.3 server.go - 의존성 주입과 라우팅

**파일**: `/Users/jupiter/openclaw/PolyON/core/internal/server/server.go`

#### 의존성 구조 (Server struct, 라인 35-55)
```go
type Server struct {
    cfg     *config.Config
    docker  *docker.Client      // 컨테이너 관리
    ldap    *ldapPkg.Client     // LDAP 클라이언트
    samba   *samba.Service      // AD 관리
    store   *store.Store        // PostgreSQL
    builder *builder.Builder    // 사이트 빌더
    gitea   *gitea.Client       // Git 저장소
    traefik *traefik.Manager    // 동적 프록시
    engines *engine.Registry    // 서비스 엔진들
    
    // 개별 서비스 클라이언트들
    driveProvisioner *nextcloudEngine.Provisioner
    mattermostClient *mattermostEngine.Client
    odooClient       *odooEngine.Client
    // ... 추가 클라이언트들
}
```

#### 초기화 순서 (New, 라인 58-150)
1. **Docker 클라이언트** (K8s 환경에서는 nil 허용)
2. **LDAP 클라이언트** 
3. **Samba 서비스** (Docker exec 사용)
4. **PostgreSQL 스토어** (15회 재시도 + 백오프)
5. **DB 인프라 서비스 적용** (URL 오버라이드)
6. **Strapi DB 생성** (백그라운드)
7. **도메인 기본값 자동 설정** (프로비저닝 완료 시)
8. **각종 엔진 등록** (Odoo, Mattermost, AFFiNE 등)

#### API 라우팅 등록 (buildRouter, 라인 166-260)
```go
r.Route("/api/v1", func(r chi.Router) {
    api.RegisterUsers(r, deps)      // 사용자 관리
    api.RegisterGroups(r, deps)     // 그룹 관리  
    api.RegisterMail(r, deps)       // 메일 관리
    api.RegisterApps(r, deps)       // 앱 관리
    api.RegisterSettings(r, deps)   // 설정 관리
    // ... 30+ API 그룹
})
```

#### ForwardAuth 통합 (라인 240-250)
- Traefik의 SSO 토큰 검증용
- DB 기반 `service_base_domain` 리졸버 설정
- `/api/internal/auth` 엔드포인트

### 3.4 API handlers 구조

**디렉토리**: `/Users/jupiter/openclaw/PolyON/core/internal/api/`
**파일 수**: 43개 핸들러 파일

#### 주요 API 그룹들
1. **사용자/디렉토리**: users.go, groups.go, ous.go (LDAP 기반)
2. **메일 관리**: mail.go (25KB), mail_history.go (Stalwart API)
3. **앱 관리**: apps.go (17KB), sso.go (SSO 설정)
4. **시스템**: system.go, settings.go, credentials.go
5. **인프라**: containers.go, databases.go, firewall.go
6. **모니터링**: sentinel.go, alerts.go  
7. **BPMN**: bpmn.go (15KB, Operaton 프록시)
8. **AI**: ai.go (13KB), automation.go (n8n)
9. **사이트 빌더**: sites.go (27KB), sites_git.go
10. **백업**: backup.go (12KB)

#### deps.go - 의존성 정의
```go
type Deps struct {
    Cfg              *config.Config
    Docker           *docker.Client
    LDAP             *ldapPkg.Client
    Samba            *samba.Service
    Store            *store.Store
    // ... 25개 의존성
}
```

---

## 4. K8s 리소스 현황

### 4.1 배포된 Pod 현황

```
READY   STATUS    AGE
polyon-auth-b9db68679-cn8f5               1/1     Running   4h12m  # Keycloak
polyon-console-7846577bcb-7n96s           1/1     Running   8m56s  # React SPA
polyon-core-54ff699569-9r5ll              1/1     Running   4h11m  # Go API  
polyon-db-0                               1/1     Running   4h13m  # PostgreSQL
polyon-dc-0                               1/1     Running   4h12m  # Samba AD
polyon-mail-0                             1/1     Running   4h11m  # Stalwart
polyon-redis-0                            1/1     Running   4h13m  # Redis
polyon-rustfs-0                           1/1     Running   4h12m  # Object Storage
polyon-search-0                           1/1     Running   4h13m  # OpenSearch
polyon-pgweb-74dfc5f7-bgx2p               1/1     Running   16m    # DB 도구
polyon-redis-commander-7bc5c5d784-xhszm   1/1     Running   24m    # Redis 도구  
polyon-elasticvue-69d6cfc7bd-x6vjw        1/1     Running   24m    # ES 도구
```

#### Pod 특징 분석
- **StatefulSet**: 상태를 가진 서비스들 (db, dc, mail, redis, rustfs, search)
- **Deployment**: 무상태 서비스들 (auth, console, core, 관리도구들)
- **ReplicaSet 히스토리**: console은 11번의 재배포 (라인 32-46)

### 4.2 ConfigMap 구조

```
NAME                   DATA   AGE
polyon-common-config   26     4h13m    # 공통 환경변수 
polyon-console-nginx   1      4h11m    # nginx 설정
polyon-db-init         1      4h13m    # DB 초기화
polyon-mail-config     1      4h11m    # Stalwart 설정
```

#### polyon-common-config 상세
- **26개 환경변수**: 도메인, URL, 포트 정보
- **envFrom 참조**: 모든 서비스에서 공통 사용
- **레거시 호환**: `SAMBA_HOST`, `KEYCLOAK_URL` 별칭 포함

### 4.3 Secret 구조

```
NAME                   TYPE                DATA   AGE
polyon-common-secret   Opaque              15     4h13m    # 서비스별 비밀번호
polyon-db-secret       Opaque              1      4h13m    # DB 전용
polyon-dc-secret       Opaque              1      4h12m    # AD 전용
polyon-rustfs-secret   Opaque              2      4h13m    # Object Storage
polyon-tls             kubernetes.io/tls   2      4h13m    # 와일드카드 인증서
```

#### Secret 참조 체인 분석
- **polyon-common-secret**: 15개 값, 모든 서비스 참조
- **개별 Secret**: 서비스별 추가 비밀값 (중복 최소화)
- **통합 비밀번호**: Operator 생성 24자리 독립 비밀번호

### 4.4 Ingress 구조

```
NAME                     HOSTS               PORTS     AGE
polyon-auth-ingress      auth.cmars.com      80, 443   4h11m
polyon-console-ingress   console.cmars.com   80, 443   4h11m  
polyon-portal-ingress    portal.cmars.com    80, 443   4h11m
```

#### 외부 접근 경로
- **Console**: `https://console.cmars.com` → polyon-console:80
- **Auth**: `https://auth.cmars.com` → polyon-auth:8080
- **Portal**: `https://portal.cmars.com` (향후 포털용)

---

## 5. 현재 문제점 분석

### 5.1 nginx.conf 이중 관리 문제

#### 문제점
- **이미지 내장**: `/Users/jupiter/openclaw/PolyON/console/nginx.conf`
- **ConfigMap**: `polyon-console-nginx`
- **현재 상태**: 두 파일 내용이 동일하지만 **분리된 관리**

#### 영향
- 설정 변경 시 두 곳을 수정해야 함
- 일관성 보장 어려움
- 배포 시 ConfigMap이 이미지 내장 파일을 오버라이드

#### 해결방안
1. **ConfigMap 단일화**: 이미지에서 nginx.conf 제거, ConfigMap만 사용
2. **빌드타임 통합**: 이미지 빌드 시 ConfigMap 템플릿 사용

### 5.2 Secret 참조 불일치

#### 현재 상황
- **polyon-common-secret**: 15개 통합 비밀번호
- **polyon-db-secret**: DB 전용 (1개 값)
- **기타 개별 Secret**: dc, rustfs 등

#### 잠재적 문제
- 비밀번호 참조 경로가 복잡함
- 일부 서비스에서 잘못된 Secret 참조 가능성
- 비밀번호 순환(rotation) 시 복잡도 증가

#### 권장사항
- Secret 참조 매핑 문서화
- 통합 vs 분리 정책 명확화

### 5.3 Dashboard API 호출 실패

#### 원인 분석
Console의 Dashboard 페이지에서 다음 API들이 실패할 가능성:
1. `/api/v1/system/status` - 시스템 상태
2. `/api/v1/containers` - 컨테이너 현황  
3. `/api/v1/monitoring/alerts` - 알림 현황
4. `/api/v1/mail/stats` - 메일 통계

#### 해결 필요사항
- API 엔드포인트별 응답 상태 점검
- 권한/인증 문제 확인
- CORS 설정 검증

### 5.4 DB 도구 접근 문제

#### 현재 상태
- **pgweb**: `/pgweb/` → `polyon-pgweb:8081` ✅ 실행 중
- **redis-commander**: `/redis/` → `polyon-redis-commander:8081` ✅ 실행 중  
- **elasticvue**: `/elasticvue/` → `polyon-elasticvue:8080` ✅ 실행 중

#### 잠재적 이슈
- Elasticsearch 보안 설정 (인증 없음)
- DB 접근 권한 관리 필요

### 5.5 서비스 간 의존성 문제

#### 시작 순서 의존성
1. **DB 우선**: PostgreSQL, Redis
2. **인증 서비스**: DC (LDAP), Keycloak
3. **Core API**: DB + LDAP 의존
4. **Console**: Core + Keycloak 의존

#### 현재 K8s readiness 체크
- 각 Pod의 헬스체크는 개별적
- 서비스 간 의존성 순서 보장 없음

---

## 6. 아키텍처 다이어그램

### 6.1 배포 구성도

```
┌─────────────────────┐    ┌─────────────────────┐
│   Operator (Go)     │    │   Traefik Ingress   │
│   - Setup Wizard    │    │   - SSL Termination │
│   - K8s Deployment  │    │   - Domain Routing  │
│   - Provisioning    │    │   - ForwardAuth     │
└─────────────────────┘    └─────────────────────┘
           │                           │
           ▼                           ▼
┌─────────────────────────────────────────────────┐
│              K8s Cluster (polyon namespace)      │
├─────────────────────┬───────────────────────────┤
│  StatefulSets       │  Deployments              │
│  - polyon-db (PG)   │  - polyon-auth (KC)       │
│  - polyon-dc (AD)   │  - polyon-console (React) │
│  - polyon-mail      │  - polyon-core (Go API)   │
│  - polyon-redis     │  - DB 관리 도구들         │
│  - polyon-search    │                           │
│  - polyon-rustfs    │                           │
└─────────────────────┴───────────────────────────┘
```

### 6.2 네트워크 흐름도

```
User Browser
     │
     ▼
[console.cmars.com] ──Ingress──► polyon-console (nginx)
     │                                    │
     ├─ /api/ ──────────────────────────► polyon-core:8000
     ├─ /auth/ ─────────────────────────► polyon-auth:8080
     ├─ /pgweb/ ────────────────────────► polyon-pgweb:8081
     ├─ /redis/ ────────────────────────► polyon-redis-commander:8081
     └─ /elasticvue/ ───────────────────► polyon-elasticvue:8080

polyon-core
     │
     ├─ DB ─────────────────────────────► polyon-db:5432
     ├─ Cache ──────────────────────────► polyon-redis:6379
     ├─ LDAP ───────────────────────────► polyon-dc:389
     ├─ Search ─────────────────────────► polyon-search:9200
     └─ Mail API ───────────────────────► polyon-mail:8080
```

### 6.3 인증 플로우

```
Console (React) 
     │ 
     ▼
Keycloak OIDC (PKCE)
     │ realm: admin
     │ client: polyon-console
     ▼
Token ──────────► Core API
     │                 │
     │                 ▼
     │          ForwardAuth
     │          (Traefik)
     │                 │
     ▼                 ▼
LDAP Sync ◄──── polyon-dc
(polyon realm)        │
                     ▼
              Samba AD Users
```

---

## 7. 결론 및 개선사항

### 7.1 현재 상태 요약

#### 잘 구성된 부분
1. **명확한 배포 단계**: 인프라 → 서비스 순서
2. **독립적 비밀번호**: 서비스별 24자리 생성
3. **통합 ConfigMap**: 환경변수 중앙 관리
4. **모듈화된 API**: 43개 핸들러로 기능 분리
5. **동적 메뉴**: 설치된 서비스만 표시

#### 개선 필요 부분
1. **nginx.conf 이중화**: ConfigMap 단일화
2. **Secret 참조 복잡도**: 매핑 정책 명확화  
3. **Dashboard API**: 실패 원인 분석
4. **의존성 순서**: K8s initContainer 활용
5. **문서화**: API 엔드포인트 명세

### 7.2 우선순위 수정사항

#### High Priority
1. ✅ nginx.conf 통합 (ConfigMap 우선)
2. ✅ Dashboard API 오류 해결
3. ✅ Secret 참조 매핑 정리

#### Medium Priority  
1. ✅ 서비스 간 의존성 순서 보장
2. ✅ API 문서화 (OpenAPI)
3. ✅ 모니터링 개선

#### Low Priority
1. ✅ 성능 최적화
2. ✅ 추가 기능 개발

### 7.3 시스템 안정성 평가

**현재 점수**: ⭐⭐⭐⭐☆ (4.0/5.0)

- **배포 자동화**: ⭐⭐⭐⭐⭐ 완성도 높음
- **설정 관리**: ⭐⭐⭐⭐☆ 약간의 중복 존재
- **보안**: ⭐⭐⭐⭐☆ OIDC + LDAP 통합 양호
- **확장성**: ⭐⭐⭐⭐☆ 모듈식 아키텍처
- **운영성**: ⭐⭐⭐☆☆ 문서화 개선 필요

---

**최종 검토일**: 2026-03-06  
**분석 완료 시각**: 15:42 GMT+9  
**다음 검토 권장**: 2주 후
