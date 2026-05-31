# Research Notes: Political / Influence Graph Expansion

> **Status**: Exploratory research — NOT an active implementation spec.
> These notes explore what it would take to extend FATBABY beyond SEC filings into a
> general political-influence intelligence platform (lobbying networks, regulatory revolving
> door, political donor patterns). None of this is currently in scope; it maps to
> `northstar.md` Phase 4 future work (Influence & Security Enrichment).
>
> The examples use a California political network (Richie Ross / Gale Kaufman / Willie Brown)
> to illustrate graph topology concepts. The architecture translates directly to FATBABY's
> existing entity-graph if this direction is ever pursued.

---

To transform a structured, data-driven Financial Intelligence Platform (which analyzes structured fields like transaction logs, SEC filings, and corporate holdings) into a General/Political Intelligence Platform capable of mapping networks like the Richie Ross-Gale Kaufman shadow power matrix, you must evolve your system's underlying architecture.A financial platform looks at transactions (Money $\rightarrow$ Asset). A political intelligence platform must map leverage, legacy, and influence (Relationships $\rightarrow$ Influence $\rightarrow$ Policy $\rightarrow$ Capital Flow).1. The Architectural Pivot: Entity-Attribute-Value (EAV) to Knowledge GraphFinancial platforms rely heavily on relational tables (users, transactions, assets). A general intelligence platform requires a Knowledge Graph (using Neo4j or AWS Neptune) where Relationships are first-class citizens with properties (weights, types, and durations).In your current database structure, a relationship is just a foreign key. In a political network, the relationship itself contains the metadata.[Entity: Human] ───(Type: MENTOR, Status: ACTIVE, Duration: 36y)───► [Entity: Human]
 (Richie Ross)                                                       (Willie Brown)
Graph Schema EvolutionTo ingest lobbyist networks, your schema must map six primary node categories and their corresponding edge properties:Nodes (Entities):Individual (Lobbyists, Consultants, Staffers, Politicians).Organization (Unions, Corporate PACS, Indian Tribes, Shell Companies).Asset (Lithium Brine Projects, Real Estate, Industrial Permits).Docket/Bill (State Legislation, Regulatory Rulings, Environmental Exemptions).Location (Addresses like 1303 J Street, campaign war rooms).Edges (Relationships):PROTÉGÉ_OF / MENTOR_OF (Inherited power).FINANCED_BY (Campaign contributions, undisclosed debt write-offs).REPRESENTED_BY (Lobbying engagements).RIVAL_OF (Negative correlation constraints).2. Information Extraction Engine (OSINT & Scraping Pipeline)How do you programmatically extract deep profile data (like Willie Brown’s monthly lunch partners or a $100,000 uncollected debt)? You must shift your data ingestion pipeline from API polling to multi-source scraping and Unstructured-to-Structured Natural Language Processing (NLP).Ingestion VectorsState Lobbying Disclosure Portals (The Foundation):Scraping state systems (e.g., California Secretary of State's Cal-Access or equivalent state lobbying logs).Target data: Form 460 (Recipient Committee Campaign Statements), Form 615 (Lobbyist Report), and Form 625 (Lobbying Firm Report).What this yields: Direct links between Richie Ross, the United Farm Workers (UFW), and the California Business Roundtable.Fair Political Practices Commission (FPPC) Enforcement dockets:Automated scraping of warning letters, stipulation agreements, and administrative fines.What this yields: The $5,000 fine for the uncollected debt from Assemblyman Paul Fong.Hyper-Local Political Journalism & Archive Processing:Continuous scraping of regional political outlets (e.g., Sacramento Bee, CalMatters, Capitol Weekly) alongside historical archives.What this yields: Quotes regarding internal rivalries, staffing lineages (who worked in whose office before becoming a consultant), and social ties (lunches, funerals, weddings).Unstructured Parsing Pipeline (LLM + NER)You pass this raw text data through a Named Entity Recognition (NER) pipeline combined with a tailored Large Language Model (LLM) structure. The extraction prompt enforces an explicit JSON format that directly feeds your Graph database:JSON{
  "source_document": "FPPC_Case_2014_Fong_Ross",
  "extracted_entities": [
    {"id": "ind_richie_ross", "type": "Individual", "name": "Richie Ross"},
    {"id": "ind_paul_fong", "type": "Individual", "name": "Paul Fong"}
  ],
  "extracted_relationships": [
    {
      "source": "ind_richie_ross",
      "target": "ind_paul_fong",
      "type": "CREDITOR_OF",
      "properties": {
        "amount": 100000,
        "status": "FORGIVEN_UNLAWFULLY",
        "regulatory_action": "FPPC_FINE_5000"
      }
    }
  ]
}
3. Network Analysis (Ranking Power Algorithmically)Once your graph is populated with thousands of lobbyists, politicians, and corporate clients, how does your system automatically rank Richie Ross or Gale Kaufman as "Tier S (The Shadow Brokers)"? You cannot rely on manual curation; you must implement graph topology algorithms.1. Betweenness Centrality (Identifying "The Fixers")Betweenness centrality measures how often a node acts as a bridge along the shortest path between two other nodes.Application: A corporate entity (e.g., Chevron) wants to reach a progressive Democratic legislator. They cannot cross the bridge directly. The algorithm flags Richie Ross because his node has a massive betweenness centrality index—he bridges the "Labor Movement" cluster and the "Big Oil" cluster.2. Degree Centrality & Eigenvector Centrality (Identifying "The Sovereigns")Degree Centrality counts how many direct connections a node has (e.g., Willie Brown’s vast tree of former staffers).Eigenvector Centrality scores a node high if it is connected to other nodes that are themselves highly connected. Kaufman scores immensely high here because her primary client is the California Teachers Association (CTA), which holds an enormous centrality score due to its $300M spending power and 310,000 members.3. Co-occurrence and Conflict MappingBy tracking when individuals appear on the same campaign filings or on opposing ballot measures, the system maps alliances and rivalries. If Node A and Node B are historically on opposing sides of 90% of high-spend ballot propositions, the system automatically draws a RIVAL_OF edge with a high conflict coefficient.4. Designing the "Shadow Map" Interface (The Back Office)To expose this to operators looking for an entry strategy (e.g., advancing a lithium project), the platform must present a clear, tactical interface. True to the institutional, archival look required for executive systems, it drops modern visualizations for highly structured Network Ledgers and Pathfinding Matrices.The Pathfinding Query InterfaceInstead of searching for a stock ticker, an operator inputs an Origin Node (Your Company/Project) and a Target Destination (The Decision Maker or Regulatory Agency, e.g., the State Water Resources Control Board).The system executes a shortest-path algorithm (like Dijkstra's or A*) through the political graph, outputting the exact Influence Chains illustrated in your strategy:[QUERY]: Find Path [Origin: Lithium Project] ──► [Destination: Nancy Pelosi]

[PATHWAY 1 FOUND - Weight: 0.92]
  Lithium Project 
    └─► [RETAINED] ──► David Roberti (Former Senate Pro Tem)
    └─► [CONTEMPORARY] ──► Richie Ross (Fixer Tier S)
    └─► [FORMER CHIEF] ──► Willie Brown (The Godfather)
    └─► [LUNCH PARTNER] ──► Nancy Pelosi (Target)
By transitioning your data layer to an interconnected graph structure, setting up text-extraction pipelines for regulatory and journalistic archives, and running centrality algorithms, you scale your platform from simple financial asset tracking to an ecosystem that maps the hidden human plumbing of real-world power.
