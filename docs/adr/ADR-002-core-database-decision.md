#### ADR-002: Use PostgreSQL instead of MongoDB for Core Data

##### Status
Accepted

##### Context
NeuralVault requires storing users, workspaces, permissions,  
AI providers, processing jobs, and metadata.
The system contains highly related entities and requires  
data consistency for operations such as access control,  
billing, and job execution.
PostgreSQL and MongoDB were evaluated as possible databases.

##### Decision
PostgreSQL will be used as the primary database for the core system.
The decision was made because the application contains  
well-defined relationships between entities and benefits  
from transactional guarantees and relational modeling.

##### Consequences

###### Positive
- Strong ACID transactions  
- Relational modeling fits the domain  
- Mature ecosystem  
- Powerful SQL querying  
- Good support for indexes and analytics  
- Possibility to use pgvector in the future — see ADR-003, which evaluates pgvector as a vector storage alternative and documents why a dedicated engine (Qdrant) was chosen first

###### Negative
- Less flexible schema evolution  
- More upfront database design  
- Horizontal scaling can be more complex than document databases