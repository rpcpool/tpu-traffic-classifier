# TPU traffic classifier


This small program creates ipsets and iptables rules for nodes in the Solana network.

By default, it creates and maintains the following ipsets:

 - `solana-unstaked`: unstaked nodes visible in gossip
 - `solana-staked`: staked nodes visible in gossip
 - `solana-high-staked`: nodes visible in gossip with >1% of stake

These sets will be kept up to date for as long as this software runs. On exit it will clean up the sets.

It also uses the PREROUTING tables to permanently mark traffic from these sets of IPs on the local nodes . This can be used in later traffic rules. By default the following fwmarks are set:

 - `1`: unstaked
 - `3`: staked
 - `9`: high staked

##  Running

`go run ./main.go`

## Traffic shaping

## Firewalling

You can add rules to `solana-tpu-custom`. For instance if you wanted to temporarily close TPU port you can run:

```
iptables -I solana-tpu-custom -j DROP
```

This will drop all traffic to the tpu port.

If you would like to drop all traffic to TPU port apart from validators (staked nodes):

```
iptables -I solana-tpu-custom -m set --match-set solana-staked -j ACCEPT
iptables -I solana-tpu-custom -m set --match-set solana-highstaked -j ACCEPT
iptables -I solana-tpu-custom -j DROP
```

If you would only allow nodes in gossip to send to your TPU:

```
iptables -I solana-tpu-custom -m set --match-set solana-staked -j ACCEPT
iptables -I solana-tpu-custom -m set --match-set solana-highstaked -j ACCEPT
iptables -I solana-tpu-custom -m set --match-set solana-unstaked -j ACCEPT
iptables -I solana-tpu-custom -j DROP
```


## Example iptables generated chains

```
*filter
:INPUT ACCEPT [0:0]
:FORWARD DROP [0:0]
:OUTPUT ACCEPT [0:0]
:solana-tpu - [0:0]
:solana-tpu-custom - [0:0]
-A INPUT -p udp -m udp --dport 8004 -j solana-tpu
-A solana-tpu -j solana-tpu-custom
COMMIT
```

```
*mangle
:PREROUTING ACCEPT [0:0]
:solana-nodes - [0:0]
-A PREROUTING -p udp -m udp --dport 8004 -j solana-nodes
-A solana-nodes -m set --match-set solana-high-staked src -j MARK --set-xmark 0x9/0xffffffff
-A solana-nodes -m set --match-set solana-staked src -j MARK --set-xmark 0x3/0xffffffff
-A solana-nodes -m set --match-set solana-unstaked src -j MARK --set-xmark 0x1/0xffffffff
COMMIT
```
