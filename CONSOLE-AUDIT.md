# Console 전수 조사 보고서 — 2026-03-06

## 1. 현재 메뉴 구조 (MODULES in ConsoleLayout.tsx)

| 메뉴 | Section | K8s 상태 | 판정 |
|------|---------|----------|------|
| Dashboard | - | ✅ 동작 | **유지** — 데이터 표시 확인 |
| Applications | - | ⚠️ 부분 | **수정** — SSO/앱 목록은 Core DB 기반, Homepage는 미구현 |
| Directory (Users/Groups/OUs/Computers/Tree) | DIRECTORY | ✅ 동작 | **유지** — LDAP 연동 정상 |
| Drive 현황 | DIRECTORY | ❌ 미구현 | **비활성** — Nextcloud 미설치 |
| Mail | SERVICES | ⚠️ 부분 | **수정** — Stalwart 연동 필요, 일부 페이지 미연동 |
| **Mattermost (Chat)** | SERVICES | ❌ 미설치 | **비활성** — Pod 없음 |
| **AI Platform** | SERVICES | ❌ 미설치 | **비활성** — polyon-ai Pod 없음 |
| **Automation (n8n)** | SERVICES | ❌ 미설치 | **비활성** — polyon-n8n Pod 없음 |
| **BPMN (Operaton)** | SERVICES | ❌ 미설치 | **비활성** — polyon-operaton Pod 없음 |
| Networking (DNS/DCs/VPN/Firewall) | INFRASTRUCTURE | ⚠️ 부분 | **수정** — DNS/DCs는 DC 연동, VPN/Firewall은 Docker 의존 |
| **Containers** | INFRASTRUCTURE | ❌ Docker 전용 | **삭제/교체** — Docker API 사용, K8s에서 무의미 |
| **Database (PG/Redis/ES)** | INFRASTRUCTURE | ❌ 깨진 UI | **수정** — iframe 방식 → K8s Service 연동으로 교체 |
| Monitoring | INFRASTRUCTURE | ⚠️ 부분 | **수정** — Grafana 미설치, Sentinel Docker 의존 |
| Security | GOVERNANCE | ⚠️ 부분 | **수정** — OPA 미설치, 감사로그는 ES 연동 |
| Settings | SYSTEM | ⚠️ 부분 | **수정** — "Docker Compose" 표시, HELIOS_ADMIN_API_SECRET 에러 |

## 2. Docker 의존 코드 목록

| 파일 | 문제 | 조치 |
|------|------|------|
| `Containers/ContainersPage.tsx` | Docker API 직접 호출 | **K8s Pod 목록으로 교체** |
| `Containers/ContainersTopologyPage.tsx` | Docker 컨테이너 의존 관계 | **K8s 토폴로지로 교체** |
| `Containers/ContainersResourcesPage.tsx` | Docker Volumes | **K8s PVC/PV로 교체** |
| `Settings/SettingsPage.tsx` | "Docker Compose" 하드코딩 | **"Kubernetes" 변경** |
| `Dashboard/QuickActions.tsx` | "Docker 상태 확인" 텍스트 | **"서비스 상태 확인" 변경** |
| `AI/AISettingsPage.tsx` | docker-compose 명령어 안내 | **kubectl 명령어로 변경** |
| `AI/AIMemoryPage.tsx` | docker-compose 참조 | **K8s 설정 안내로 변경** |
| Core: `internal/docker/` | Docker SDK Client | **K8s client-go로 교체 (Phase 3)** |
| Core: `internal/api/backup.go` | `docker cp` 명령 | **kubectl cp 또는 PVC 직접 접근** |
| Core: `internal/api/firewall.go` | Docker socket 접근 | **K8s NetworkPolicy로 교체** |
| Core: `internal/api/system.go` | `Docker.ContainerList()` | **K8s Pod 목록으로 교체** |

## 3. HELIOS 잔재

| 위치 | 내용 | 조치 |
|------|------|------|
| `styles/app.scss:526` | "HELIOS Design Override" 주석 | PolyON으로 변경 |
| Settings 페이지 | `HELIOS_ADMIN_API_SECRET` 환경변수 참조 | `POLYON_ADMIN_API_SECRET`으로 변경 |

## 4. 메뉴 동적화 설계 (Phase 2 핵심)

### Core API 신규 엔드포인트: `GET /api/v1/platform/services`

```json
{
  "services": [
    {
      "id": "directory",
      "name": "Directory",
      "category": "base",
      "status": "running",
      "required": true
    },
    {
      "id": "mail",
      "name": "Mail",
      "category": "base",
      "status": "running",
      "required": true
    },
    {
      "id": "chat",
      "name": "Mattermost",
      "category": "app",
      "status": "not_installed",
      "required": false
    }
  ]
}
```

### Console 메뉴 로직 변경

```
1. Console 로드 시 /api/v1/platform/services 호출
2. status !== "not_installed" 인 서비스만 메뉴에 표시
3. "not_installed" 서비스는 Applications 페이지에 "설치 가능" 으로 표시
4. Settings 페이지의 시스템 정보는 K8s API (kubectl) 기반으로 변경
```

## 5. 작업 분배

### @S (Sonnet) — Core API 수정
1. `GET /api/v1/platform/services` 신규 엔드포인트 (K8s Pod 스캔 기반)
2. `internal/api/system.go` — Docker → K8s Pod 목록
3. `internal/api/backup.go` — docker cp → kubectl cp
4. Containers API → K8s Pods/Services/PVC API로 교체
5. `HELIOS_ADMIN_API_SECRET` → `POLYON_ADMIN_API_SECRET`

### @G (Ganymede) — Console UI 수정
1. ConsoleLayout.tsx 메뉴 동적화 (/api/v1/platform/services 기반)
2. 미설치 서비스 페이지에 "서비스 미설치" 안내 + 설치 버튼 표시
3. Containers 페이지 → K8s Pods/Services 뷰로 교체
4. Database 페이지 iframe 제거 → K8s Service 상태 + 연결 정보 표시
5. Docker 참조 텍스트 전부 K8s로 변경
6. "example.com" 하드코딩 → domainInfo.realm 동적 바인딩
7. HELIOS 잔재 치환

## 6. 우선순위 (보스 지시 반영)

P0 (즉시): 메뉴 동적화 + 깨진 페이지 수정
P1 (금일): Docker 의존 코드 K8s 전환
P2 (추후): 미설치 서비스 설치 UI (Applications 카탈로그)
