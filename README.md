# :wave: HeyFil

Crawls FileCoin state market participants, gathers statistics on their state as an index provider,
and announces their latest advertisement to IPNI network.

The `heyfil` service periodically:
* lists FileCoin state market participants
* checks if they are reachable
* checks if they are an index provider on a given topic
* checks if they expose a non-empty head advertisement, and
* announces any non-empty heads to a configured IPNI node.

In addition, the service exposes a set of Prometheus metrics to report
* number of participants by their status, and
* total number of participants

The reported status of a state market participants are:

| status              | Description                                                                                                                         |
|---------------------|-------------------------------------------------------------------------------------------------------------------------------------|
| `ok`                | Participant is an index provider with non-empty head successfully announced to IPNI.                                                |
| `err-api-call`      | Failure occurred while calling the FileCoin API caused by communication error.                                                      |
| `err-internal`      | Failed to get participant's address which was not a communication or RPC error response.                                            |
| `err-rpc-unknown`   | Failed to get participant's address caused by RPC error response.                                                                   |
| `not-miner`         | The participant is not a miner.                                                                                                     |
| `unreachable`       | The participant was not reachable via their address.                                                                                |
| `unaddressable`     | No address was found for the participant.                                                                                           |
| `unindexed`         | The participant is reachable but is not an IPNI index provider.                                                                     |
| `topic-mismatch`    | The participant is reachable and an IPNI index provider but not on the expected topic.                                              |
| `empty-head`        | The participant is reachable and an IPNI index provider on the expected topic but has no advertisements.                            |
| `err-get-head`      | The participant is reachable and an IPNI index provider on the expected topic but failure occured when fetching head advertisement. |
| `err-announce`      | Failed to announce the index provider to the configured IPNI node                                                                   | 
| `unknown`           | Could not be determined; most likely caused by a bug or missing condition check.                                                    |


## Install
```shell
go install github.com/ipni/heyfil
```

## License

[SPDX-License-Identifier: Apache-2.0 OR MIT](LICENSE.md)
