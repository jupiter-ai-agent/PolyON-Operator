# PolyON Console 기능 상태 점검 보고서

**점검 일시:** 2026-03-06 16:00 KST  
**점검자:** Jupiter AI  
**환경:** Kubernetes namespace `polyon`, Console v1.3.0  

## 📊 전체 요약

- **정상 API:** 7개 (70%)
- **404 에러 API:** 6개 (60%)
- **서비스 연결 실패:** 1개 (Docker 연결)
- **nginx 프록시:** 일부 경로 정상, 일부 404

## 🔍 API 엔드포인트 상세 점검

### ✅ 정상 작동 (200 OK)

| API 엔드포인트 | 상태 | 응답 요약 |
|---|---|---|
| `/api/v1/users` | 200 | 3개 사용자 반환 (Guest, Administrator, krbtgt) |
| `/api/v1/groups` | 200 | 38개 그룹 반환 (도메인 관리자, 사용자 등) |
| `/api/v1/ous` | 200 | 1개 OU 반환 (Domain Controllers) |
| `/api/v1/ous/tree` | 200 | 완전한 OU 트리 구조 반환 |
| `/api/v1/domain/dcs` | 200 | DC1.cmars.com 정보 반환 |
| `/api/v1/domain/info` | 200 | 도메인 기본 정보 반환 (cmars.com) |
| `/api/v1/apps` | 200 | 23개 앱 정보 반환 (Mail, Auth, HR, Chat 등) |

### ❌ 404 에러 발생

| API 엔드포인트 | 상태 | 문제 분석 |
|---|---|---|
| `/api/v1/computers` | 404 | Core API에서 라우트 미구현 |
| `/api/v1/system/status` | 404 | Core API에서 라우트 미구현 |
| `/api/v1/mail/overview` | 404 | Core API에서 라우트 미구현 |
| `/api/v1/settings` | 404 | Core API에서 라우트 미구현 |
| `/api/v1/dns/records` | 404 | Core API에서 라우트 미구현 |
| `/api/sentinel/state` | 404 | **polyon-operator 서비스 누락** |

### ⚠️ 연결 오류

| API 엔드포인트 | 상태 | 응답 내용 |
|---|---|---|
| `/api/v1/containers` | 200 | "Cannot connect to the Docker daemon" (예상됨, K8s 환경) |

## 🌐 nginx 프록시 라우팅 점검

### ✅ 정상 프록시

| 경로 | 대상 서비스 | 상태 | 비고 |
|---|---|---|---|
| `/api/` | polyon-core:8000 | 정상 | Core API 라우팅 작동 |
| `/elasticvue/` | polyon-elasticvue:8080 | 정상 | HTML 응답 확인됨 |

### ❌ 문제 발견

| 경로 | 대상 서비스 | 상태 | 문제점 |
|---|---|---|---|
| `/api/sentinel/` | polyon-operator:1110 | **서비스 없음** | operator pod/service 미배포 |
| `/auth/` | polyon-auth:8080 | 404 | 서비스는 존재하나 라우팅 문제 |
| `/pgweb/` | polyon-pgweb:8081 | 404 (루트) | `/pgweb/` 경로에서는 정상 |
| `/redis/` | polyon-redis-commander:8081 | 404 (루트) | 서비스별 경로 설정 필요 |

## 🛠 ConfigMap vs 실제 설정

**비교 결과:** ConfigMap `polyon-console-nginx`와 실제 pod 내 `/etc/nginx/conf.d/default.conf`가 **완전히 일치**함.

## 🔧 핵심 문제점 및 원인 분석

### 1. 🚨 polyon-operator 서비스 누락 (Critical)

**문제:**
- `polyon-operator` pod/service가 배포되지 않음
- `/api/sentinel/state` 등 operator API 엔드포인트 모두 404

**영향:**
- Sentinel 상태 확인 불가
- 시스템 설정/리셋 기능 불가
- 시스템 모니터링 기능 제한

**해결 방법:**
```bash
# polyon-operator 배포 상태 확인 및 재배포 필요
kubectl get deployment polyon-operator -n polyon
```

### 2. ⚠️ Core API 미구현 라우트 (Medium)

**문제:**
- 여러 API 엔드포인트가 Core API에서 구현되지 않음
- `/api/v1/{computers,system/status,mail/overview,settings,dns/records}`

**영향:**
- Dashboard 일부 정보 표시 불가
- 시스템 상태 모니터링 불가
- 설정 관리 기능 사용 불가

**해결 방법:**
- Core API 코드 검토하여 누락된 핸들러 구현
- 또는 해당 기능들이 다른 서비스로 이관되었는지 확인

### 3. ℹ️ DB 도구 라우팅 설정 (Low)

**문제:**
- pgweb, redis-commander가 루트 경로(`/`)에서 404
- 각 도구들이 특정 base path에서만 작동하도록 설정됨

**영향:**
- 직접 서비스 접근 시 404 (실제 사용에는 영향 없음)

## 📋 권장 수정 사항

### 1. 즉시 수정 (High Priority)

1. **polyon-operator 배포 복구**
   ```bash
   # operator 배포 상태 확인
   kubectl get all -n polyon | grep operator
   
   # 필요시 operator 재배포
   kubectl apply -f polyon-operator-deployment.yaml
   ```

### 2. 중기 수정 (Medium Priority)

2. **Core API 누락 엔드포인트 구현**
   - `computers` API 구현 
   - `system/status` API 구현
   - `mail/overview` API 구현
   - `settings` API 구현
   - `dns/records` API 구현

### 3. 장기 개선 (Low Priority)

3. **DB 도구 Base Path 설정**
   - pgweb, redis-commander의 base path 설정 검토
   - 필요시 각 도구의 설정 파일 수정

## 🏥 현재 사용 가능한 기능

- ✅ 사용자/그룹 관리
- ✅ OU 관리 
- ✅ 도메인 정보 확인
- ✅ 앱 목록/상태 확인
- ✅ Elasticsearch 관리 (Elasticvue)
- ⚠️ PostgreSQL 관리 (pgweb - `/pgweb/` 경로로만)
- ⚠️ Redis 관리 (일부 제한)

## 🚫 현재 사용 불가 기능

- ❌ 시스템 상태 모니터링
- ❌ Sentinel 상태 확인
- ❌ 컴퓨터 목록 관리
- ❌ 메일 개요 확인
- ❌ 시스템 설정 관리
- ❌ DNS 레코드 관리

---

**최종 권장사항:** polyon-operator 서비스 복구가 최우선이며, 이후 Core API의 누락된 엔드포인트들을 단계적으로 구현해야 합니다.