
Pro modelování bude vždy použito některé z následujících úrovní detailu popisu (granularity):

```text
L0 – katalog systému
  systém, vlastník, kritičnost, RTO/RPO, business služba

L1 – hlavní závislosti
  klíčové aplikace, databáze, identity provider, storage, DNS, síťové služby

L2 – aplikační rozpad
  frontend, backend, worker, batch, API, fronty, datové objekty

L3 – provozní infrastruktura
  clustery, VM, namespace, ingress, storage class, lokality, síťové zóny

L4 – technický detail
  deploymenty, services, VLANy, CIDR, VIP, významné IP adresy, backup policies

L5 – nízkoúrovňová konfigurace
  jednotlivé IP všech uzlů, porty switchů, firewall rules, konkrétní pody
```

Pro základní modelování přehledu systému bude použito **L1 až L3**, u kritických systémů  **L4**. Úroveň **L5** nebude přímo v modelu, jedná se o konkrétní konfigurace v jednotlivých systémech. Může však být s Archimate modelem propojena např. pomocí identifikátorů apod. 

Praktické pravidlo pro zahrnutí komponenty nebo informace do modelu je následující:

```text
Do modelu patří objekt, pokud:
- je důležitý pro pochopení architektury,
- má dopad na dostupnost nebo obnovu,
- je sdílenou závislostí,
- má vlastníka nebo odpovědnost,
- je cílem dotazů typu „co se stane při výpadku X?“,
- nebo má samostatný DR význam.
```

Model nebude obsahovat informace o objektech, pokud:

```text
- je jen implementační detail,
- často se automaticky mění,
- je snadno získatelný z jiného source of truth,
- nemá samostatný dopad,
- nikdo nad ním nebude dělat rozhodnutí.
```

Například `Autentizační server`, `PostgreSQL DB`, `Oracle DB`, `DC1`, `APP_NET`, `WAN mezi DC`, `F5 Ingress` do modelu patří. Jednotlivý dočasný pod, každá IP worker nodu nebo každá firewall rule obvykle ne. Ale VIP load balanceru, DNS resolver nebo Kubernetes API endpoint už ano, protože jsou to významné stabilní body závislosti.

Při modelování se tedy vždy bude pracovat s  **granularity policy**. Každá komponenta bude modelována s konkrétní hloubkou: „katalog“, „DR minimum“, „aplikační detail“, „infrastrukturní detail“. Nejprve se vždy popisují základní vrstvy a poté se jde do hloubky. 

Klíčové bude, aby granularita nebyla globální, ale **závislá na kritičnosti systému**. Pro málo důležitý systém může stačit L1. Pro business kritický systém s RTO 4 hodiny bude potřeba L3/L4. Tím se udrží rozumná náročnost a zároveň model poskytne dost detailu tam, kde je to skutečně potřeba.

Pro modelování konkrétních hloubek a úrovní granularity v Archimate bude využíván následující přístup. 


# L0 – katalog systému

**Účel:** vědět, proč systém existuje, kdo za něj odpovídá a jak důležitý je.

### Prvky

| ArchiMate prvek        | Použití                                                |
| ---------------------- | ------------------------------------------------------ |
| `BusinessService`      | služba poskytovaná organizaci nebo zákazníkům          |
| `BusinessActor`        | vlastník, organizace nebo odpovědný útvar              |
| `BusinessRole`         | business owner, system owner, provozní role            |
| `ApplicationComponent` | systém jako jeden celek                                |
| `Requirement`          | RTO, RPO, dostupnost nebo jiné závazné požadavky       |
| `Assessment`           | kritičnost nebo základní hodnocení stavu               |
| `Location`             | základní informace o provozní lokalitě, je-li důležitá |

### Vazby

```text
Assignment
Realization
Serving
Association
Influence
```

### Typický model

```text
BusinessActor --Assignment--> BusinessRole
BusinessRole --Assignment--> BusinessService
ApplicationComponent --Realization--> BusinessService
Requirement --Association--> ApplicationComponent
Assessment --Influence--> Requirement
```

### Povinné atributy

```yaml
owner:
technical_owner:
criticality:
lifecycle_state:
rto:
rpo:
description:
```

L0 by měl být vyplnitelný během několika minut a měl by odpovědět:

```text
Co je to za systém?
Jakou službu poskytuje?
Kdo za něj odpovídá?
Jak důležitý je?
Jaké má RTO/RPO?
```

---

# L1 – hlavní služby a závislosti

**Účel:** umožnit základní dopadovou a DR analýzu.

L1 obsahuje vše z L0 a přidává:

### Prvky

| ArchiMate prvek        | Použití                                      |
| ---------------------- | -------------------------------------------- |
| `BusinessProcess`      | klíčový proces podporovaný systémem          |
| `ApplicationService`   | funkce/služba poskytovaná aplikací           |
| `ApplicationInterface` | významné uživatelské nebo systémové rozhraní |
| `DataObject`           | hlavní business nebo aplikační data          |
| `TechnologyService`    | databáze, identita, DNS, storage, messaging  |
| `Facility`             | datacentrum nebo fyzický provozní areál      |
| `Risk`                 | známé riziko nebo single point of failure    |

### Vazby

```text
Realization
Serving
Access
Flow
Association
Assignment
Composition
Aggregation
Influence
```

### Typický model

```text
BusinessProcess --Serving/Used-by--> BusinessService
ApplicationService --Serving--> BusinessProcess
ApplicationComponent --Realization--> ApplicationService
ApplicationComponent --Access--> DataObject
TechnologyService --Serving--> ApplicationComponent
ApplicationComponent --Flow--> ApplicationComponent
Facility --Association--> ApplicationComponent
Risk --Influence--> BusinessService
```

### Povinné atributy vazeb

```yaml
dependency_type:
dependency_strength: mandatory | optional
degraded_mode:
business_impact:
```

L1 by měl umět odpovědět:

```text
Co přestane fungovat při výpadku systému?
Na kterých sdílených službách závisí?
Která data jsou kritická?
Existuje omezený provozní režim?
```

---

# L2 – aplikační a datový rozpad

**Účel:** popsat vnitřní strukturu systému, integrační vazby a datovou odpovědnost.

L2 obsahuje vše z L0–L1 a přidává:

### Prvky

| ArchiMate prvek            | Použití                                           |
| -------------------------- | ------------------------------------------------- |
| `ApplicationComponent`     | frontend, backend, worker, scheduler, mikroslužba |
| `ApplicationFunction`      | interní logická funkce komponenty                 |
| `ApplicationProcess`       | aplikační workflow nebo dávkové zpracování        |
| `ApplicationEvent`         | významná aplikační událost                        |
| `ApplicationCollaboration` | skupina komponent spolupracujících jako celek     |
| `ApplicationInterface`     | API, message endpoint, uživatelské UI             |
| `DataObject`               | transakční data, audit, konfigurace, index, cache |

### Vazby

```text
Composition
Aggregation
Assignment
Realization
Serving
Access
Flow
Triggering
Specialization
```

### Typický model

```text
System --Composition--> Frontend
System --Composition--> Backend
System --Composition--> Worker

Backend --Realization--> ApplicationService
Backend --Assignment--> ApplicationFunction
ApplicationProcess --Triggering--> ApplicationProcess

Backend --Access(read/write)--> TransactionData
Worker --Access(write)--> AuditData

Backend --Flow(HTTPS)--> ExternalSystem
Backend --Flow(Kafka)--> Worker
```

### Atributy

Na rozhraních a vazbách:

```yaml
protocol:
port:
synchronous:
authentication:
timeout:
retry_policy:
message_or_object:
```

Na datových objektech:

```yaml
data_class:
master_source:
retention:
rebuildable:
backup_required:
rpo:
classification:
```

L2 by měl odpovědět:

```text
Z jakých komponent se systém skládá?
Jaká API a integrační toky používá?
Která komponenta čte nebo zapisuje která data?
Které části lze obnovit nezávisle?
```

---

# L3 – provozní a technologická infrastruktura

**Účel:** popsat nasazení aplikací, runtime, clustery, servery, storage a lokality.

L3 obsahuje vše z nižších úrovní a přidává:

### Prvky

| ArchiMate prvek       | Použití                                           |
| --------------------- | ------------------------------------------------- |
| `Node`                | VM, cluster, logický runtime uzel                 |
| `Device`              | fyzický server, appliance, síťové zařízení        |
| `SystemSoftware`      | OS, Kubernetes, databázový runtime, middleware    |
| `TechnologyService`   | compute, storage, DNS, IAM, load balancing        |
| `TechnologyInterface` | významný technologický endpoint                   |
| `Artifact`            | image, balíček, Helm chart, konfigurační artefakt |
| `Facility`            | datacentrum                                       |
| `Location`            | město, site nebo geografická oblast               |

### Vazby

```text
Assignment
Realization
Serving
Composition
Aggregation
Association
Deployment
Access
Flow
```

Poznámka: v datovém metajazyce lze používat výraz `deployedOn`, ale při exportu do čistého ArchiMate se převede na odpovídající standardní kombinaci prvků a vztahů.

### Typický model

```text
ApplicationComponent --deployedOn--> SystemSoftware
SystemSoftware --Assignment--> Node
Node --Composition/Aggregation--> Device
Node --Association--> Facility
Facility --Association--> Location

Artifact --Deployment--> Node
TechnologyService --Serving--> ApplicationComponent
TechnologyInterface --Serving--> ApplicationComponent
```

### Atributy

```yaml
environment:
cluster:
namespace:
runtime:
replica_count:
ha_mode:
primary_site:
dr_site:
deployment_source:
external_reference:
```

L3 by měl odpovědět:

```text
Kde aplikace skutečně běží?
Je runtime redundantní?
Které systémy sdílejí cluster nebo storage?
Co ovlivní výpadek konkrétního datacentra?
Jak se aplikace znovu nasadí?
```

---

# L4 – síťový a detailní infrastrukturní kontext

**Účel:** zachytit síťové zóny, komunikační sítě, významné endpointy a provozní cesty potřebné pro bezpečnostní a DR analýzu.

L4 obsahuje vše z L0–L3 a přidává:

### Prvky

| ArchiMate prvek        | Použití                                                          |
| ---------------------- | ---------------------------------------------------------------- |
| `CommunicationNetwork` | VLAN, subnet, DMZ, WAN, storage nebo backup síť                  |
| `TechnologyInterface`  | VIP, API endpoint, síťové rozhraní významné služby               |
| `TechnologyService`    | DNS, VPN, firewalling, routing, load balancing                   |
| `Device`               | firewall, router, load balancer, významný switch                 |
| `Path`                 | logická nebo fyzická komunikační cesta                           |
| `Node`                 | bastion, gateway, cluster endpoint                               |
| `Artifact`             | síťová politika nebo deklarativní konfigurace, pokud je významná |

### Vazby

```text
Flow
Serving
Association
Composition
Aggregation
Assignment
Realization
Access
```

### Typický model

```text
ApplicationComponent --Flow--> ApplicationComponent
Flow {
  source_zone = APP_NET
  target_zone = DB_NET
  protocol = TCP
  port = 5432
}

Node --Association--> CommunicationNetwork
Device --Serving--> CommunicationNetwork
TechnologyService --Realization--> Device
Path --Association--> CommunicationNetwork
```

### Atributy

Na sítích:

```yaml
vlan_id:
cidr:
zone:
site:
routing_domain:
source_of_truth:
external_id:
```

Na významných rozhraních:

```yaml
fqdn:
vip:
ip_address:
protocol:
port:
certificate_reference:
```

Na tocích:

```yaml
source_zone:
target_zone:
protocol:
port:
direction:
firewall_policy_reference:
dependency_strength:
degraded_mode:
```

L4 by měl odpovědět:

```text
Kudy probíhá kritická komunikace?
Které systémy ovlivní výpadek VLAN nebo WAN?
Která komunikace prochází mezi bezpečnostními zónami?
Které VIP, DNS nebo gateway endpointy jsou single point of failure?
```

---

# Souhrnná matice

| Prvek                       |  L0 |  L1 |  L2 |  L3 |  L4 |
| --------------------------- | :-: | :-: | :-: | :-: | :-: |
| BusinessActor               |  ✓  |  ✓  |  ✓  |  ✓  |  ✓  |
| BusinessRole                |  ✓  |  ✓  |  ✓  |  ✓  |  ✓  |
| BusinessService             |  ✓  |  ✓  |  ✓  |  ✓  |  ✓  |
| BusinessProcess             |     |  ✓  |  ✓  |  ✓  |  ✓  |
| ApplicationComponent        |  ✓  |  ✓  |  ✓  |  ✓  |  ✓  |
| ApplicationService          |     |  ✓  |  ✓  |  ✓  |  ✓  |
| ApplicationInterface        |     |  ✓  |  ✓  |  ✓  |  ✓  |
| ApplicationFunction/Process |     |     |  ✓  |  ✓  |  ✓  |
| DataObject                  |     |  ✓  |  ✓  |  ✓  |  ✓  |
| TechnologyService           |     |  ✓  |  ✓  |  ✓  |  ✓  |
| Node                        |     |     |     |  ✓  |  ✓  |
| Device                      |     |     |     |  ✓  |  ✓  |
| SystemSoftware              |     |     |     |  ✓  |  ✓  |
| TechnologyInterface         |     |     |     |  ✓  |  ✓  |
| Artifact                    |     |     |     |  ✓  |  ✓  |
| Facility                    |     |  ✓  |  ✓  |  ✓  |  ✓  |
| Location                    |  ✓  |  ✓  |  ✓  |  ✓  |  ✓  |
| CommunicationNetwork        |     |     |     |     |  ✓  |
| Path                        |     |     |     |     |  ✓  |
| Requirement                 |  ✓  |  ✓  |  ✓  |  ✓  |  ✓  |
| Assessment                  |  ✓  |  ✓  |  ✓  |  ✓  |  ✓  |
| Risk                        |     |  ✓  |  ✓  |  ✓  |  ✓  |
| Gap                         |     |     |     |  ✓  |  ✓  |
| WorkPackage                 |     |     |     |  ✓  |  ✓  |

## Doporučený praktický profil

Za základ bych považoval:

```text
L0: povinný pro každý evidovaný systém
L1: povinný pro produkční systémy
L2: pro integračně nebo datově významné systémy
L3: pro kritické systémy a DR plánování
L4: jen pro síťově, bezpečnostně nebo provozně významné části
```

Důležité je také nevynucovat jednu úroveň pro celý model. Systém může být popsán na L3, zatímco sdílený Keycloak jen na L1 a kritická WAN cesta na L4. Granularita by tedy měla být vlastností konkrétní části modelu, ne pouze celého projektu.

