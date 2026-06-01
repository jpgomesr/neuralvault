#### ADR-001: Why use Go for NeuralVault core instead of Java?

##### Status
Accepted

##### Context
The NeuralVault core is responsible for coordinating AI providers,  
storage operations, indexing, and file processing.
The application may run on self-hosted environments with limited  
resources. Lower memory consumption and simpler deployment are  
important requirements.
Java and Go were evaluated as possible technologies for the core.

##### Decision
Go will be used as the primary language for the NeuralVault core.
The decision was made because Go produces a single executable binary,  
has low memory overhead, starts quickly, and provides sufficient  
performance for I/O-intensive workloads.

##### Consequences

###### Positive
- Lower memory consumption  
- Simple deployment through a single binary  
- Fast startup time  
- Easy containerization  
- Strong concurrency support through goroutines

###### Negative
- Smaller ecosystem compared to Java  
- Fewer mature enterprise frameworks  
- Team members familiar with Java may require adaptation