# Console 페이지별 API 의존성 맵

**작성**: Jupiter (팀장) · 2026-03-06  
**목적**: 각 페이지가 어떤 API를 호출하는지 정확히 파악하여, 변경 시 영향 범위를 사전에 알 수 있게 함

---

## 범례

- ✅ Core에 라우트 등록 확인
- ⚠️ Core에 라우트 미확인 (404 가능)
- 🔌 외부 서비스 프록시 (nginx → 개별 서비스)
- `apiFetch` = `/api/v1` prefix + Bearer token
- `fetch` = 직접 호출 (prefix 포함하여 명시)

---

## 1. Dashboard (`/`)

**파일**: `pages/Dashboard/DashboardPage.tsx` + `components/`

| 컴포넌트 | API 호출 | Core 라우트 | 비고 |
|----------|---------|------------|------|
| StatCards | `apiFetch('/users')` | ✅ `api.RegisterUsers` | 사용자 수 |
| StatCards | `apiFetch('/groups')` | ✅ `api.RegisterGroups` | 그룹 수 |
| StatCards | `apiFetch('/ous')` | ✅ `api.RegisterOUs` | OU 수 |
| StatCards | `apiFetch('/computers')` | ⚠️ **미확인** — `/domain/computers` 있음, `/computers` 없음 | **불일치 가능** |
| DomainInfoCard | `apiFetch('/domain/info')` | ✅ `api.RegisterDomain` | |
| SystemResourcesCard | `apiFetch('/system/resources')` | ✅ `api.RegisterSystem` | |
| MailServiceCard | `apiFetch('/mail/status')` | ✅ `api.RegisterMail` | |
| MailServiceCard | `apiFetch('/mail/service-check')` | ✅ `api.RegisterMail` | |
| ServiceStatusCard | `apiFetch('/containers/')` | ✅ `api.RegisterContainers` | |
| AlertsCard | `fetch('/api/alerts/summary')` | ✅ `/api/alerts` 라우트 | |
| AlertsCard | `fetch('/api/alerts?limit=5')` | ✅ `/api/alerts` 라우트 | |
| AlertsCard | `apiFetch('/sentinel/status')` | ✅ `api.RegisterSentinel` | |
| DB StatusCard | `apiFetch('/databases/status')` | ✅ `api.RegisterDatabases` | |
| DB StatusCard | `apiFetch('/databases/rustfs/stats')` | ✅ `api.RegisterDatabases` | |
| MailServiceCard | `fetch('/api/v1/mail/principals?...')` | ✅ `api.RegisterMail` | Stalwart 프록시 |
| MailServiceCard | `fetch('/api/v1/mail/queue?...')` | ✅ `api.RegisterMail` | Stalwart 프록시 |
| SystemResourcesCard | `prometheusQueryRange(...)` | ✅ `/system/prometheus/query_range` | |

### Dashboard 잠재 문제
- **`/computers`**: `dashboard.ts`는 `apiFetch('/computers')` 호출하지만, Core 라우트는 `/domain/computers`. **404 반환 가능**

---

## 2. Directory — Users (`/users`)

**파일**: `pages/Users/UsersPage.tsx`, `api/users.ts`

| 기능 | API 호출 | Core 라우트 |
|------|---------|------------|
| 목록 | `GET /api/v1/users` | ✅ |
| 상세 | `GET /api/v1/users/{username}` | ✅ |
| 생성 | `POST /api/v1/users` | ✅ |
| 수정 | `PUT /api/v1/users/{username}` | ✅ |
| 삭제 | `DELETE /api/v1/users/{username}` | ✅ |
| 비밀번호 | `POST /api/v1/users/{username}/password` | ✅ |
| 활성화 | `POST /api/v1/users/{username}/enable` | ✅ |
| 비활성화 | `POST /api/v1/users/{username}/disable` | ✅ |
| 사진 조회 | `GET /api/v1/users/{username}/photo` | ✅ |
| 사진 업로드 | `PUT /api/v1/users/{username}/photo` | ✅ |
| 사진 삭제 | `DELETE /api/v1/users/{username}/photo` | ✅ |
| 이동 | `POST /api/v1/users/{username}/move` | ⚠️ **미확인** — Core에 move 라우트 없을 수 있음 |

---

## 3. Directory — Groups (`/groups`)

**파일**: `api/users.ts` (groups 섹션)

| 기능 | API 호출 | Core 라우트 |
|------|---------|------------|
| 목록 | `GET /api/v1/groups` | ✅ |
| 상세 | `GET /api/v1/groups/{name}` | ✅ |
| 생성 | `POST /api/v1/groups` | ✅ |
| 수정 | `PATCH /api/v1/groups/{name}` | ⚠️ Core는 `PUT` 등록? `PATCH` 확인 필요 |
| 삭제 | `DELETE /api/v1/groups/{name}` | ✅ |
| 멤버 추가 | `POST /api/v1/groups/{name}/members` | ✅ |
| 멤버 제거 | `DELETE /api/v1/groups/{name}/members/{username}` | ✅ |

---

## 4. Directory — OUs (`/ous`)

| 기능 | API 호출 | Core 라우트 |
|------|---------|------------|
| 목록 | `GET /api/v1/ous` | ✅ |
| 트리 | `GET /api/v1/ous/tree` | ✅ |
| 내용 | `GET /api/v1/ous/contents?dn=...` | ✅ |
| 생성 | `POST /api/v1/ous` | ✅ |
| 삭제 | `DELETE /api/v1/ous?dn=...` | ✅ |

---

## 5. Directory — Computers (`/computers`)

**파일**: `api/users.ts`

| 기능 | API 호출 | Core 라우트 |
|------|---------|------------|
| 목록 | `GET /api/v1/domain/computers` | ⚠️ Core에서 `/domain/computers` 확인 필요 |
| DCs | `GET /api/v1/domain/dcs` | ✅ `api.RegisterDomain` |

---

## 6. Directory Tree (`/tree`)

**파일**: `pages/Directory/TreePage.tsx`

- `useAppStore` → store에서 데이터 읽음 (별도 API 호출 없음, 또는 OU tree 재사용)

---

## 7. Mail (`/mail`, `/mail/*`)

**파일**: `pages/Mail/*.tsx`, Core `api/mail.go`

| 페이지 | API 호출 | Core |
|--------|---------|------|
| 개요 | `GET /api/v1/users` | ✅ (사용자 목록으로 메일 계정 매칭) |
| 계정 | `GET /api/v1/users` | ✅ |
| 계정 | `GET /api/v1/mail/principals?...` | ✅ Stalwart 프록시 |
| 도메인 | `POST /api/v1/mail/provision` | ✅ |
| 수신이력 | `GET /api/v1/mail/history?...` | ✅ `api.RegisterMailHistory` |
| 발송이력 | `GET /api/v1/mail/history?...` | ✅ |
| 라이브 트레이싱 | `GET /mail-proxy/logs/live?limit=100` | ✅ `/mail-proxy` 라우트 (server.go:386) |

**나머지 Mail 페이지** (groups, lists, queue, reports, roles, tenants, api-keys, oauth, sieve, spam, tls, config, maintenance, troubleshoot):
- 대부분 Stalwart Admin API 프록시 (`/api/v1/mail/*`) 경유
- Core `api/mail.go`가 Stalwart에 전달

---

## 8. Database Tools (`/databases/*`)

**파일**: `pages/Databases/*.tsx`

| 페이지 | API 호출 | nginx 프록시 |
|--------|---------|-------------|
| PostgreSQL | `GET /api/v1/databases/status` | ✅ Core |
| PostgreSQL | iframe: `/pgweb/` | 🔌 → `polyon-pgweb:8081` |
| Redis | `GET /api/v1/databases/status` | ✅ Core |
| Redis | iframe: `/redis/` | 🔌 → `polyon-redis-commander:8081` |
| Elasticsearch | `GET /api/v1/databases/status` | ✅ Core |
| Elasticsearch | iframe: `/elasticvue/` | 🔌 → `polyon-elasticvue:8080` |
| Elasticsearch | `/es-proxy/*` | 🔌 nginx → `polyon-search:9200` |

---

## 9. Containers (`/containers`)

| 페이지 | API 호출 | Core |
|--------|---------|------|
| 목록 | `GET /api/v1/pods` | ⚠️ Core 라우트 확인 — `RegisterContainers`가 `/pods` 등록하는지 |
| 로그 | `GET /api/v1/pods/{name}/logs?tail=100` | ⚠️ |
| 재시작 | `POST /api/v1/pods/{name}/restart` | ⚠️ |
| 토폴로지 | `GET /api/v1/services/topology` | ⚠️ |
| 리소스 | `GET /api/v1/containers/volumes` | ⚠️ |

---

## 10. Networking (`/dns`, `/dcs`)

| 페이지 | API 호출 | Core |
|--------|---------|------|
| DNS | `GET /api/v1/dns/zones` | ✅ `api.RegisterDNS` |
| DNS | `GET /api/v1/dns/zones/{zone}/records` | ✅ |
| DNS | `POST/PUT/DELETE /api/v1/dns/zones/{zone}/records` | ✅ |
| DCs | `GET /api/v1/domain/info` | ✅ |
| DCs | `GET /api/v1/containers/` | ✅ |
| DCs | `GET /api/v1/domain/fsmo` | ✅ `api.RegisterDomain` |
| DCs | `GET /api/v1/dns/domain/level` | ✅ |
| DCs | `GET /api/v1/domain/replication` | ✅ |
| Firewall | `GET /api/v1/firewall/services` | ✅ `api.RegisterFirewall` |

---

## 11. Monitoring (`/monitoring`)

| 페이지 | API 호출 | Core |
|--------|---------|------|
| 대시보드 | `GET /api/v1/system/prometheus/query?...` | ✅ |
| 대시보드 | `GET /api/v1/databases/status` | ✅ |
| Alerts | `GET /api/v1/system/prometheus/alerts` | ✅ |
| Alerts | `GET /api/v1/alert-rules` | ✅ |
| Alerts | `POST/PUT/DELETE /api/v1/alert-rules/...` | ✅ |
| Logs | `GET /api/v1/containers/{svcId}/logs?tail=500` | ⚠️ `/containers/{id}/logs` 확인 필요 |
| Sentinel | `GET /api/v1/sentinel/alerts?limit=50` | ✅ |
| Sentinel | `GET /api/v1/sentinel/stats` | ✅ |
| Sentinel | `GET /api/v1/sentinel/config` | ✅ |
| Sentinel | `GET /api/v1/sentinel/container` | ✅ |
| Sentinel | `GET /api/v1/sentinel/status` | ✅ |
| Sentinel | `GET /api/v1/sentinel/events` | ✅ |

---

## 12. Security (`/security`)

| 페이지 | API 호출 | Core |
|--------|---------|------|
| 비밀번호 정책 | `GET /api/v1/security/password-policy` | ✅ `api.RegisterSecurity` |
| 비밀번호 정책 | `PUT /api/v1/security/password-policy` | ✅ |
| ACL | `GET /api/v1/security/acl/ous` | ✅ |
| ACL | `GET /api/v1/security/acl?dn=...` | ✅ |
| GPO | `GET/POST/DELETE /api/v1/security/gpo` | ✅ |
| GPO link | `POST /api/v1/security/gpo/link` | ✅ |
| 접근정책 | `GET /api/v1/policy/status` | ✅ `api.RegisterPolicy` |
| 접근정책 | `GET /api/v1/policy/roles` | ✅ |
| 접근정책 | `POST /api/v1/policy/test` | ✅ |
| 감사로그 | `GET /api/v1/system/audit?...` | ✅ |

---

## 13. Settings (`/settings`)

| 페이지 | API 호출 | Core |
|--------|---------|------|
| 일반 | `api.RegisterSettings` | ✅ |
| 도메인 | `api.RegisterSettings` | ✅ |
| TLS | `api.RegisterSettings` | ✅ |
| 인증정보 | `api.RegisterCredentials` | ✅ |
| SMTP | `api.RegisterSMTP` | ✅ |
| 시스템정보 | `fetch('/auth/realms/master')` | 🔌 nginx → polyon-auth:8080 |
| 리셋 | `fetch('/api/sentinel/state')` | ✅ `/api/setup` + `/api/reset` 라우트 |

---

## 14. Apps (`/apps`)

| 기능 | API 호출 | Core |
|------|---------|------|
| 목록 | `GET /api/v1/apps` | ✅ `api.RegisterApps` |
| 설치/시작/중지 | `POST /api/v1/apps/{id}/install\|start\|stop` | ✅ |
| SSO | `api.RegisterSSO` | ✅ |
| Homepage Sites | `GET /api/v1/sites` | ✅ `api.RegisterSites` |

---

## 15. AI/Chat/Automation/BPMN (엔진 서비스)

모두 Core의 엔진 프록시 경유:
- **AI**: `/api/v1/ai/*` → LiteLLM 프록시 (✅ `api.RegisterAI`)
- **Chat**: `/api/v1/engines/chat/*` → Mattermost 프록시 (✅ `api.RegisterChat`)
- **Automation**: `/api/v1/engines/automation/*` → n8n 프록시 (✅ `api.RegisterAutomation`)
- **BPMN**: `/api/v1/engines/bpmn/*` → Operaton 프록시 (✅ `api.RegisterBPMN`)

---

## 16. Setup (`/setup`)

| 기능 | API 호출 | Core |
|------|---------|------|
| 준비 | `POST /api/sentinel/prepare` | ✅ `/api/setup` → Operator 프록시 |
| 설치 | `POST /api/sentinel/setup` | ✅ |
| 진행상태 | `GET /api/sentinel/progress` | ✅ |

---

## 🔴 확인된 잠재 문제 요약

| # | 문제 | 영향 페이지 | 심각도 |
|---|------|-----------|--------|
| 1 | `apiFetch('/computers')` — Core에 `/computers` 직접 라우트 없음, `/domain/computers` 있음 | Dashboard (StatCards), Computers 페이지 | **High** |
| 2 | `/api/v1/pods`, `/api/v1/pods/{name}/logs`, `/api/v1/pods/{name}/restart` — K8s 전환 후 Docker 기반 컨테이너 API가 작동하는지 미확인 | Containers, Monitoring Logs | **Medium** |
| 3 | `/api/v1/services/topology` — 등록 여부 미확인 | Containers Topology | **Medium** |
| 4 | `/api/v1/containers/volumes` — 등록 여부 미확인 | Containers Resources | **Medium** |
| 5 | `PATCH /api/v1/groups/{name}` — Core가 PUT만 등록했을 수 있음 | Groups 수정 | **Low** |
| 6 | `/api/v1/users/{username}/move` — 등록 여부 미확인 | Users OU 이동 | **Low** |

---

## nginx 프록시 의존성 (Console → 외부 서비스)

| nginx location | 대상 서비스 | 사용 페이지 |
|---------------|-----------|-----------|
| `/api/` | `polyon-core:8000` | 전체 |
| `/auth/` | `polyon-auth:8080` | Settings 시스템정보, 인증 |
| `/pgweb/` | `polyon-pgweb:8081` | DB - PostgreSQL |
| `/redis/` | `polyon-redis-commander:8081` | DB - Redis |
| `/elasticvue/` | `polyon-elasticvue:8080` | DB - Elasticsearch |
| `/es-proxy/` | `polyon-search:9200` | DB - Elasticsearch (auto-config) |
| `/mail-proxy/` | Core 경유 → Stalwart | Mail Live Tracing |

**핵심**: nginx.conf 수정 시, 위 7개 프록시 중 하나라도 빠지면 해당 페이지 전체가 깨짐.

---

## 사용 방법

코드 변경 전 이 문서를 확인:
1. 변경 대상 파일이 어떤 페이지에 영향을 주는지 확인
2. 해당 페이지의 API 의존성 확인
3. 변경 후 해당 API 전부 테스트
4. nginx.conf 변경 시 모든 location 블록 테스트
