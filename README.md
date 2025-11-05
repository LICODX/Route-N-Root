# RNR Protocol Implementation

The RNR project provides a robust, Go-based implementation of a blockchain node, featuring a sophisticated consensus mechanism that combines Proof-of-Burn (PoB) and Verifiable Random Functions (VRF) with elements of Proof-of-History (PoH) and Zero-Knowledge Proofs (zkProofs). It includes comprehensive modules for P2P networking, secure wallet management, blockchain state management, a transaction mempool, and a flexible API. The project also integrates advanced monitoring capabilities with Prometheus and Grafana, along with various utilities for operational tasks and deployment.

## Project Structure

*   **`build-cross-platform.sh`**: A script to compile the project for various operating systems and architectures.
*   **`rnr.zip`**: Likely a pre-built distribution archive of the RNR node.
*   **`go.mod` and `go.sum`**: Go module definition files managing project dependencies.
*   **`cmd/`**: Contains the main entry points for executable applications, including the core `rnr` node (`cmd/rnr`) and a `genesis` utility (`cmd/genesis`) for creating the initial blockchain state.
*   **`pkg/`**: Houses the core Go packages that encapsulate the project's logic:
    *   **`pkg/api/`**: Defines the public API server for interacting with the node.
    *   **`pkg/blockchain/`**: Manages the core blockchain ledger, including state, fork resolution, and transaction mempool integration.
    *   **`pkg/consensus/`**: Implements the advanced consensus protocols, notably Proof-of-Burn (PoB), Verifiable Random Functions (VRF), Proof-of-History (PoH), governance, and validator management with features like slashing and checkpointing.
    *   **`pkg/core/`**: Provides fundamental data types, constants, and core interfaces.
    *   **`pkg/genesis/`**: Handles the creation and management of the blockchain's genesis block.
    *   **`pkg/logging/`**: Provides structured logging functionalities.
    *   **`pkg/mempool/`**: Manages the pool of unconfirmed transactions before inclusion in a block.
    *   **`pkg/metrics/`**: Integrates with Prometheus for exposing monitoring metrics.
    *   **`pkg/network/`**: Implements the P2P communication layer, including node discovery, gossip protocols, authentication, IP reputation, and rate limiting.
    *   **`pkg/sync/`**: Manages synchronization mechanisms within the node.
    *   **`pkg/utils/`**: Contains common utility functions, such as health checks, error handling, and graceful shutdown procedures.
    *   **`pkg/wallet/`**: Provides functionalities for managing cryptographic keys and user wallets.
*   **`scripts/`**: A collection of shell scripts for common operational tasks like node startup and genesis block creation.
    *   **`scripts/deployment/`**: Specific scripts for building, deploying, and managing multi-node setups and VPS environments, including health checks.
*   **`tools/`**: Contains administrative utilities, such as scripts for blockchain state migration, backup, and restore.
*   **`monitoring/`**: Stores configuration files for monitoring systems:
    *   **`monitoring/grafana/`**: Grafana dashboards for visualizing node metrics.
    *   **`monitoring/prometheus/`**: Prometheus alert rules for critical events.