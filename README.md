# ledger

Basic implementations of ledger single-threaded approach live here. 

Suggested interfaces for building ledger database in [interfaces.go](./interfaces.go) using types in here.

[money](./money/) package has currency oriented logic, though not appropiate for crypto since we use uint64 for amounts and that represents at most 14 ETH. Go team please add uint128 :)