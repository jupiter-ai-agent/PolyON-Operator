# PolyON 환경변수/시크릿 관리 종합 검토 보고서
**작성자:** Jupiter (팀장) | **일자:** 2026-03-06

---

## 1. 현재 구조의 문제점

### 1-1. 아키텍처 개요
```
Operator (Go) → Go template 치환 → kubectl apply -f (각 manifest)
```
- Operator가 `TemplateConfig` 구조체의 값을 `{{.AdminPassword}}` 등으로 치환하여 각 manifest에 주입
- **Secret/ConfigMap 중앙화 없음** — 각 manifest가 독립적으로 값을 보유

### 1-2. 현재 공유 변수 현황

| Go Template 변수 | 사용 횟수 | 사용 서비스 |
|------------------|----------|------------|
| `{{.AdminPassword}}` | **10회** | Core, Keycloak, Samba DC, Stalwart, RustFS, PostgreSQL |
| `{{.Namespace}}` | 28회 | 전체 |
| `{{.DomainNetBIOS}}` | 4회 | Core, Samba DC |
| `{{.DomainUpper}}` | 3회 | Samba DC |
| `{{.PostgresPassword}}` | 2회 | PostgreSQL Secret, Keycloak |
| `{{.ConsoleDomain}}` | 2회 | Ingress |
| `{{.AuthDomain}}` | 2회 | Ingress |
| `{{.PortalDomain}}` | 2회 | Ingress |
| `{{.DomainDC}}` | 2회 | Stalwart config |

### 1-3. 식별된 문제점

#### ❌ P1: 환경변수 이름 불일치 (오늘의 장애 원인)
| 서비스 | manifest 변수명 | Core가 읽는 변수명 | 결과 |
|--------|----------------|-------------------|------|
| DC Admin PW | `SAMBA_ADMIN_PASSWORD` | `ADMIN_PASSWORD` | **빈 비밀번호 → LDAP bind 실패** |
| KC Admin PW | 미설정 | `KC_ADMIN_PASSWORD` | 빈 값 |
| Stalwart Admin PW | 미설정 | `STALWART_ADMIN_PASSWORD` | 빈 값 |
| Elastic PW | 미설정 | `ELASTIC_PASSWORD` | 빈 값 |
| RustFS PW | 미설정 | `RUSTFS_ROOT_PASSWORD` | 빈 값 |
| DB PW | 미설정 (DATABASE_URL에 포함) | `DB_PASSWORD` | URL 파싱으로 동작 |

#### ❌ P2: 동일 비밀번호의 다른 이름으로 반복 사용
`{{.AdminPassword}}`가 **6개 서비스의 서로 다른 용도**에 사용됨:
- Samba DC admin 비밀번호
- Keycloak bootstrap admin 비밀번호
- Stalwart fallback admin 비밀번호
- Stalwart LDAP bind 비밀번호
- RustFS secret key
- PostgreSQL 접속 비밀번호 (DATABASE_URL 내)

→ **단일 비밀번호로 전체 시스템 접근 가능** (보안 위험)
→ **하나의 서비스만 비밀번호 변경 불가** (결합도 높음)

#### ❌ P3: Secret 미활용
- Secret을 사용하는 서비스: **PostgreSQL, Samba DC** (2개만)
- 나머지는 **env에 평문 직접 주입**: Keycloak, Core, Stalwart, RustFS
- K8s Secret은 RBAC, etcd 암호화, audit 등 보안 기능 제공 → 미활용

#### ❌ P4: ConfigMap 미활용
- 도메인, realm, NetBIOS, 서비스 URL 등 **비밀이 아닌 설정값**도 env에 직접 하드코딩
- 서비스 URL(`polyon-dc`, `polyon-auth` 등)이 각 manifest에 산재

#### ❌ P5: Core가 필요로 하는 변수 vs manifest가 제공하는 변수 괴리

Core `config.go`가 읽는 변수 (17개):
```
DATABASE_URL, DB_PASSWORD, ADMIN_PASSWORD, SAMBA_REALM, SAMBA_DOMAIN,
SAMBA_HOST, LDAP_URL, KEYCLOAK_URL, STALWART_URL, ELASTICSEARCH_URL,
RUSTFS_ENDPOINT, REDIS_URL, KC_ADMIN_PASSWORD, STALWART_ADMIN_PASSWORD,
ELASTIC_PASSWORD, RUSTFS_ROOT_PASSWORD, RUSTFS_ROOT_USER
```

Core manifest가 제공하는 변수 (14개):
```
DB_HOST, DATABASE_URL, SAMBA_REALM, SAMBA_DOMAIN, SAMBA_HOST,
SAMBA_ADMIN_PASSWORD, ADMIN_PASSWORD, LDAP_URL, KEYCLOAK_URL,
STALWART_URL, ELASTICSEARCH_URL, RUSTFS_ENDPOINT, REDIS_URL
```

**누락된 변수 (3개):**
- `KC_ADMIN_PASSWORD` — Keycloak API 호출 불가
- `STALWART_ADMIN_PASSWORD` — Stalwart API 호출 불가
- `RUSTFS_ROOT_PASSWORD` — RustFS 접근 불가

---

## 2. K8s 네이티브 방법론 제안

### 2-1. 설계 원칙

1. **단일 진실 원천 (Single Source of Truth)**: 공유 설정은 하나의 ConfigMap/Secret에만 존재
2. **서비스별 비밀번호 분리**: 각 서비스는 고유 비밀번호 보유
3. **참조만 허용**: manifest는 값을 직접 보유하지 않고, ConfigMap/Secret을 참조
4. **네이밍 컨벤션 통일**: `POLYON_` prefix + 서비스명 + 용도

### 2-2. 리소스 구조

#### `polyon-config` (ConfigMap) — 비밀이 아닌 공유 설정
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: polyon-config
  namespace: polyon
data:
  # Domain
  POLYON_DOMAIN: "cmars.com"
  POLYON_DOMAIN_UPPER: "CMARS.COM"
  POLYON_DOMAIN_NETBIOS: "CMARS"
  POLYON_DOMAIN_DC: "DC=cmars,DC=com"
  POLYON_ORG_NAME: "cmars"
  
  # Service URLs (cluster-internal)
  POLYON_DB_HOST: "polyon-db"
  POLYON_DB_PORT: "5432"
  POLYON_REDIS_URL: "redis://polyon-redis:6379"
  POLYON_LDAP_URL: "ldap://polyon-dc:389"
  POLYON_KEYCLOAK_URL: "http://polyon-auth:8080"
  POLYON_STALWART_URL: "http://polyon-mail:8080"
  POLYON_SEARCH_URL: "http://polyon-search:9200"
  POLYON_RUSTFS_URL: "http://polyon-rustfs:9000"
  
  # External domains
  POLYON_CONSOLE_DOMAIN: "console.cmars.com"
  POLYON_AUTH_DOMAIN: "auth.cmars.com"
  POLYON_PORTAL_DOMAIN: "portal.cmars.com"
  POLYON_MAIL_DOMAIN: "mail.cmars.com"
```

#### `polyon-secrets` (Secret) — 공유 비밀
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: polyon-secrets
  namespace: polyon
type: Opaque
stringData:
  # 서비스별 독립 비밀번호
  POLYON_DB_PASSWORD: "<generated>"
  POLYON_DC_ADMIN_PASSWORD: "<from setup>"
  POLYON_KC_ADMIN_PASSWORD: "<generated>"
  POLYON_STALWART_ADMIN_PASSWORD: "<generated>"
  POLYON_RUSTFS_SECRET_KEY: "<generated>"
  POLYON_SEARCH_PASSWORD: ""  # disabled security
  
  # 연결 문자열 (비밀 포함)
  POLYON_DATABASE_URL: "postgres://polyon:<db_pw>@polyon-db:5432/polyon?sslmode=disable"
```

### 2-3. 각 서비스 manifest 참조 방식

```yaml
# 예: Core Deployment
env:
  # ConfigMap에서 전체 주입
  envFrom:
    - configMapRef:
        name: polyon-config
    - secretRef:
        name: polyon-secrets
  # 또는 개별 참조 (변수명 매핑 필요 시)
  env:
    - name: ADMIN_PASSWORD
      valueFrom:
        secretKeyRef:
          name: polyon-secrets
          key: POLYON_DC_ADMIN_PASSWORD
    - name: KC_ADMIN_PASSWORD
      valueFrom:
        secretKeyRef:
          name: polyon-secrets
          key: POLYON_KC_ADMIN_PASSWORD
```

### 2-4. Operator 변경 사항

1. **Setup Wizard 완료 시**: `polyon-config` ConfigMap + `polyon-secrets` Secret 생성 (1회)
2. **각 서비스 manifest**: `envFrom`으로 참조만 (Go template에서 비밀번호 직접 치환 제거)
3. **비밀번호 생성**: Operator가 서비스별 랜덤 비밀번호 생성 → Secret에 저장
4. **DC admin 비밀번호만 사용자 입력** (Setup Wizard에서)

### 2-5. 네이밍 컨벤션

```
ConfigMap: POLYON_{CATEGORY}_{NAME}
Secret:   POLYON_{SERVICE}_{PURPOSE}

카테고리: DOMAIN, DB, REDIS, LDAP, KC(Keycloak), STALWART, SEARCH, RUSTFS
용도:     PASSWORD, SECRET_KEY, ADMIN_PASSWORD, URL
```

---

## 3. 마이그레이션 단계

| 단계 | 작업 | 영향 |
|------|------|------|
| 1 | `polyon-config` ConfigMap + `polyon-secrets` Secret 리소스 정의 | 신규 |
| 2 | Operator `provisioning.go`에서 Setup 완료 시 ConfigMap/Secret 생성 | Operator 수정 |
| 3 | 각 manifest에서 하드코딩 env 제거, `envFrom` 또는 `valueFrom` 참조로 전환 | 전체 manifest 수정 |
| 4 | Core `config.go`에서 환경변수명 통일 (POLYON_ prefix 대응) | Core 수정 |
| 5 | 서비스별 비밀번호 분리 (AdminPassword 단일값 → 서비스별 생성) | 보안 강화 |

---

## 4. 즉시 조치 필요 사항

### 4-1. 긴급 (현재 장애 유발 가능)
- [ ] Core manifest에 `KC_ADMIN_PASSWORD`, `STALWART_ADMIN_PASSWORD`, `RUSTFS_ROOT_PASSWORD` 추가
- [ ] 환경변수명 Core↔manifest 불일치 전수 해소

### 4-2. 단기 (구조 개선)
- [ ] `polyon-config` ConfigMap + `polyon-secrets` Secret 중앙화
- [ ] Operator에서 서비스별 랜덤 비밀번호 생성
- [ ] 전체 manifest `envFrom` 전환

### 4-3. 중기 (제품 품질)
- [ ] Sealed Secrets 또는 External Secrets Operator 도입 (운영 환경)
- [ ] 비밀번호 로테이션 메커니즘
- [ ] CI/CD에서 manifest 린팅 (환경변수 누락 자동 탐지)
