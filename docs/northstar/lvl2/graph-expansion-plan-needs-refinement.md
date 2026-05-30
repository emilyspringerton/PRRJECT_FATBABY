To transform the FARTHQ intelligence ecosystem from a structured financial processor (PRRJECT-FATBABY tracking SEC filings, stock tickers, and transaction logs) into a unified, graph-driven General/Political Intelligence Platform capable of mapping networks like the Sacramento shadow brokers, we require a systematic, multi-phased engineering pipeline.

This implementation plan establishes the exact milestones, data pipelines, schema migrations, and integration points with the newly upgraded IDUNA Platform IAM & Governance Service.

Phase 1: Storage Layer Evolution (The Graph Core)
Objective: Establish a hybrid data model where PRRJECT-FATBABY's relational MySQL "True Store" handles transactional state, while a newly deployed Graph Database handles influence topology.

Step 1.1: Provision and Connect the Graph DB
Deploy Neo4j Enterprise (or AWS Neptune) adjacent to the current database cluster.

Update config/database.json inside the repository to externalize graph access keys.

Step 1.2: Define the Target Schema Constraints
Execute the strict Graph schema definition via a initialization migration file (migrations/graph/001_initial_topology.cypher):

Cypher
// 1. Enforce unique constraints on primary structural nodes
CREATE CONSTRAINT FOR (i:Individual) REQUIRE i.id IS UNIQUE;
CREATE CONSTRAINT FOR (o:Organization) REQUIRE o.id IS UNIQUE;
CREATE CONSTRAINT FOR (a:Asset) REQUIRE a.id IS UNIQUE;
CREATE CONSTRAINT FOR (b:Bill) REQUIRE b.id IS UNIQUE;

// 2. Build index points for rapid pathfinding traversals
CREATE INDEX FOR (i:Individual) ON [i.name];
CREATE INDEX FOR (o:Organization) ON [o.name];
Phase 2: Building the Extraction & Ingestion Pipeline
Objective: Transition PRRJECT-FATBABY from tracking solely clean structured feeds (like SEC RSS streams) to processing messy, hyper-local public documentation, regulatory logs, and campaign finance structures.

+---------------------------------------------------------------------------------------+
|                                    INGESTION LAYER                                    |
|  [Cal-Access Lobbyist Scraper]   [FPPC Enforcement Dockets]   [Capitol Press Archives] |
+-----------------------------------------------------------+---------------------------+
                                                            |
                                                            v
+---------------------------------------------------------------------------------------+
|                                  EXTRACTION PIPELINE                                  |
|               (Go-based Worker Engine -> LLM Named Entity Recognition)                 |
+-----------------------------------------------------------+---------------------------+
                                                            | Extracted JSON Elements
                                                            v
+---------------------------------------------------------------------------------------+
|                                   TOPOLOGY INJECTION                                  |
|         (Cypher Execution: Merge Nodes, Calculate Edge Property Weights)              |
+-----------------------------------------------------------+---------------------------+
                                                            |
                                                            v
                                                   [(Neo4j Graph Database)]
Step 2.1: Write the Specialized OSINT Scrapers
Create Go-based data harvest cron daemons under cmd/scrapers/:

lobbyist_scraper.go: Programmatically polls state disclosure portals (e.g., California Secretary of State's Cal-Access) to parse Form 615 (Lobbyist Reports) and Form 625 (Lobbying Firm Reports).

fppc_scraper.go: Scrapes state ethics and fair political practice commissions for regulatory warning letters, active dockets, and settlement stipulations.

press_scraper.go: Pulls raw markdown text from regional political watchdogs (CalMatters, Capitol Weekly, Sacramento Bee).

Step 2.2: Unstructured-to-Structured LLM Parsing Engine
Build a processing worker pipeline (internal/nlp/extractor.go) that batches unstructured scrapings and sends them to your localized or protected model to perform strict Named Entity Recognition (NER).

The engine must enforce an immutable output format:

Go
package nlp

type ExtractedRelationship struct {
	SourceEntity     string                 `json:"source_entity"`
	SourceType       string                 `json:"source_type"` // Individual, Organization, etc.
	TargetEntity     string                 `json:"target_entity"`
	TargetType       string                 `json:"target_type"`
	RelationshipType string                 `json:"relationship_type"` // MENTOR_OF, FINANCED_BY, RIVAL_OF
	Properties       map[string]interface{} `json:"properties"`
}
Step 2.3: Cypher Ingestion Transformer
Write the graph loader function (internal/graph/loader.go) that converts the extracted payloads into transactional Cypher queries:

Go
func InsertRelationship(ctx context.Context, session neo4j.SessionWithContext, rel ExtractedRelationship) error {
	query := fmt.Sprintf(`
		MERGE (s:%s {name: $source_name})
		ON CREATE SET s.id = apoc.create.uuid()
		MERGE (t:%s {name: $target_name})
		ON CREATE SET t.id = apoc.create.uuid()
		MERGE (s)-[r:%s]->(t)
		SET r += $properties
	`, rel.SourceType, rel.TargetType, rel.RelationshipType)

	_, err := session.Run(ctx, query, map[string]interface{}{
		"source_name": rel.SourceEntity,
		"target_name": rel.TargetEntity,
		"properties":  rel.Properties,
	})
	return err
}
Phase 3: Implementing Graph Analytics & Network Ranking
Objective: Programmatically assign the system "Tiers" (e.g., Tier S Shadow Broker vs. Tier 2 Backstage Node) using network topology rather than human editorial opinion.

Step 3.1: Configure Graph Data Science (GDS) Workers
Write scheduled analytical jobs inside internal/analytics/graph_rank.go to compute centrality across the entire ecosystem map nightly.

Betweenness Centrality (Identifying "The Fixers"): Finds components that control the flow of influence between isolated networks (e.g., bridging corporate energy clients to labor unions, like Richie Ross).

Eigenvector Centrality (Identifying "The Sovereigns"): Measures influence based on how well-connected a node is to other major entities (e.g., mapping Gale Kaufman's immense network via her multi-decade connection to the California Teachers Association).

Step 3.2: Expose the "Shadow Map" Influence Engine API
Build an API layer (cmd/signalapi/graph_handlers.go) protected by your standard iamguard middleware that exposes tactical pathfinding endpoints.

Endpoint: GET /api/v1/influence/path

Query Params: ?origin=LithiumProject&target=NancyPelosi

Internal Action: Runs a shortest-path search matching custom edge weights (where RIVAL_OF adds massive cost friction, and MENTOR_OF or REPRESENTED_BY establishes highly conductive pathways).

Phase 4: Upgrading EMILY Prime's Cognitive Toolkit
Objective: Provide the automated agent (cmd/emily-agent) with explicit tools to query, write to, and navigate this political network map.

Step 4.1: Deploy Graph Query Tools
Add new tools to Emily’s runtime environment (cmd/emily-agent/signal_intelligence.go):

pol_map_query_path: Accepts an origin asset and target entity name, returning the shortest human influence sequence.

pol_map_get_profile: Returns the full graph node property layout, active financial backings, and recorded conflicts for a specific lobbyist or lawmaker.

Step 4.2: Update the Internal Rule Loops
Modify the inner execution loops inside Emily (cmd/emily-agent/main.go). When an anomaly is detected on an asset (e.g., an unexpected regulatory delay on a lithium brine permit), Emily is instructed to automatically:

Identify the regulatory director who signed the delay.

Run pol_map_query_path from the project asset to that director.

Isolate the key lobbying brokers controlling access to that director.

Output a formatted "Strategic Entry Memo" directly onto the back-office ledger interface.

Phase 5: UI Modernization (The Archival Back Office)
Objective: Expose the influence paths and shadow maps on the administrative panel while strictly adhering to the "Aunt Sally" visual layout rules.

Step 5.1: Build the Pathway Ledger Component
Create an operational grid window titled "Influence Conduit Matrix".

Visual Treatment: No sweeping animated visual nodes or circular cluster maps. Instead, render paths as an indented, highly dense monospace table with clean border dividers.

=============================================================================================
TARGET INFLUENCE PATHWAY: [Origin: BRINE_PROJECT_01] -> [Target: CHAIR_WATER_BOARD]
PATH CONFIDENCE SCORE: 94.2%  | RISK DEGRADATION COEFFICIENT: LOW
=============================================================================================
STEP  NODE TYPE     ENTITY NAME          RELATIONSHIP TYPE    DOCUMENTATION ANCHOR
---------------------------------------------------------------------------------------------
001   Asset         BRINE_PROJECT_01     -                    -
002   Individual    DAVID ROBERTI        [RETAINED_BY]        Form 625 (Lobbyist Firm Reg)
003   Individual    RICHIE ROSS          [CONTEMPORARY_OF]    SacBee Archive (Case #0921)
004   Individual    WILLIE BROWN         [PROTÉGÉ_OF]         Willie Brown Member Services
005   Individual    CHAIR_WATER_BOARD    [APPOINTED_BY]       Gov-Executive Order 2024-12
=============================================================================================
Step 5.2: Implement the "Nuclear Option" Strategic Simulation Prompt
Add a specialized console action utility inside the workspace dashboard. When planning major legislative actions, the operator can click a confirmation button styled in Rose Gold (#B76E79) to run a complete landscape simulation. This execution runs conflict checks through the database graph and predicts exactly how rivals (e.g., Kaufman's labor networks vs. Ross's corporate interests) will react if you attempt to hire one over the other—or if you lock down both.

Phase 6: System Hardening & IAM Compliance Validation
Objective: Secure the general intelligence platform using the centralized IDUNA framework before processing sensitive organizational intelligence.

[ ] Ensure all background data harvesting scrapers (cmd/scrapers/) are registered in IDUNA's agents directory under the type DAEMON.

[ ] Enforce that the ingestion workers write every single graph update transaction to the immutable append-only event stream table (iam_event_stream) as an AuthorityActionExecuted log entry.

[ ] Verify that all user-facing graph queries across the cmd/dashboard server environment strictly require token extraction containing the flattened permissions capability node general_intelligence.read.
