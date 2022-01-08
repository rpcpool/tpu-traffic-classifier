package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/coreos/go-iptables/iptables"
	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/nadoo/ipset"
	"gopkg.in/yaml.v3"
)

type PeerNode struct {
	Pubkey     solana.PublicKey
	GossipIp   string
	GossipPort string
	TPUIp      string
	TPUPort    string
	Stake      uint64
}

var (
	flagConfigFile           = flag.String("config-file", "config.yml", "configuration file")
	flagPubkey               = flag.String("pubkey", "", "validator-pubkey")
	flagRpcUri               = flag.String("rpc-uri", "https://api.mainnet-beta.solana.com", "the rpc uri to use")
	myGossipPort      string = ""
	myTPUPort         string = ""
	mangleChain              = "solana-nodes"
	filterChain              = "solana-tpu"
	filterChainCustom        = "solana-tpu-custom"
)

type TrafficClass struct {
	Name   string  `yaml:"name"`
	Stake  float64 `yaml:"stake_percentage"` // If it has more than this stake
	FwMark uint64  `yaml:"fwmark,omitempty"`
}

type Config struct {
	Classes       []TrafficClass `yaml:"staked_classes"`
	UnstakedClass TrafficClass   `yaml:"unstaked_class"`
}

func main() {
	flag.Parse()

	// Load traffic classes
	f, err := os.Open(*flagConfigFile)
	if err != nil {
		log.Println("couldn't open config file", *flagConfigFile, err)
		os.Exit(1)
	}

	var cfg Config
	decoder := yaml.NewDecoder(f)
	err = decoder.Decode(&cfg)
	if err != nil {
		log.Println("couldn't decode config file", *flagConfigFile, err)
		os.Exit(1)
	}

	// Special traffic class for unstaked nodes visible in gossip (e.g. RPC)
	cfg.UnstakedClass.Stake = -1
	cfg.Classes = append(cfg.Classes, cfg.UnstakedClass)

	// Sort the classes by stake weight
	sort.SliceStable(cfg.Classes, func(i, j int) bool {
		return cfg.Classes[i].Stake > cfg.Classes[j].Stake
	})

	// Connect to rpc
	client := rpc.New(*flagRpcUri)

	// Create iptables nd ipset
	ipt, err := iptables.New()
	if err != nil {
		log.Println("couldn't init iptables", err)
		os.Exit(1)
	}

	if err := ipset.Init(); err != nil {
		log.Println("error in ipset init", err)
		os.Exit(1)
	}

	// Clear the ipsets
	for _, set := range cfg.Classes {
		ipset.Create(set.Name)
		ipset.Flush(set.Name)
	}

	// Add base rules for marking packets in iptables
	exists, err := ipt.ChainExists("mangle", mangleChain)
	if err != nil {
		log.Println("couldn't check existance", mangleChain, err)
		os.Exit(1)
	}
	if !exists {
		err = ipt.NewChain("mangle", mangleChain)
		if err != nil {
			log.Println("couldn't create mangle chain", mangleChain, err)
			os.Exit(1)
		}
	}

	exists, err = ipt.ChainExists("filter", filterChain)
	if err != nil {
		log.Println("couldn't check existance", filterChain, err)
		os.Exit(1)
	}
	if !exists {
		ipt.NewChain("filter", filterChain)
		if err != nil {
			log.Println("couldn't create filter chain", filterChain, err)
			os.Exit(1)
		}
	}

	exists, err = ipt.ChainExists("filter", filterChainCustom)
	if err != nil {
		log.Println("couldn't check existance", filterChainCustom, err)
		os.Exit(1)
	}
	if !exists {
		ipt.NewChain("filter", filterChainCustom)
		if err != nil {
			log.Println("couldn't create filter chain", filterChainCustom, err)
			os.Exit(1)
		}
	}
	// Create mangle rules for all the classes
	for _, set := range cfg.Classes {
		err = ipt.AppendUnique("mangle", mangleChain, "-m", "set", "--match-set", set.Name, "src", "-j", "MARK", "--set-mark", strconv.FormatUint(set.FwMark, 10))
		if err != nil {
			log.Println("couldn't append mangle rule for set ", set.Name, err)
		}
	}

	// If there's no pubkey, then send all matching traffic to the filter chain and the mangle chain
	if flagPubkey == nil || *flagPubkey == "" {
		err := ipt.AppendUnique("mangle", "PREROUTING", "-p", "udp", "-j", mangleChain)
		if err != nil {
			log.Println("could not add prerouting mangle chain: ", err)
		}
		err = ipt.AppendUnique("filter", "INPUT", "-p", "udp", "-j", filterChain)
		if err != nil {
			log.Println("could not add input filter chain: ", err)
		}

	}
	err = ipt.Insert("filter", filterChain, 1, "-j", filterChainCustom)
	if err != nil {
		log.Println("could not add custom rules chain: ", err)
	}

	// Clean up on signals
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		<-c
		// Clean up
		for _, set := range cfg.Classes {
			ipset.Flush(set.Name)
			ipset.Destroy(set.Name)
			//ipt.Delete("mangle", mangleChain, "-m", "set", "--match-set", set.Name, "src", "-j", "MARK", "--set-mark", "4")
		}

		// We didn't find the TPU port so we never added those rules
		if myTPUPort != "" {
			ipt.Delete("mangle", "PREROUTING", "-p", "udp", "--dport", myTPUPort, "-j", mangleChain)
			ipt.Delete("filter", "INPUT", "-p", "udp", "--dport", myTPUPort, "-j", filterChain)
		}

		// Just in case, clean these rules up
		ipt.Delete("mangle", "PREROUTING", "-p", "udp", "-j", mangleChain)
		ipt.Delete("filter", "INPUT", "-p", "udp", "-j", filterChain)

		// Clear and delete these chains
		ipt.ClearAndDeleteChain("mangle", mangleChain)
		ipt.ClearAndDeleteChain("filter", filterChain)

		// Only delete the custom chain if it is empty
		ipt.DeleteChain("filter", filterChainCustom)

		os.Exit(1)
	}()

	for {
		log.Println("Updating ipsets")

		stakedNodes, err := client.GetVoteAccounts(
			context.TODO(),
			&rpc.GetVoteAccountsOpts{},
		)

		if err != nil {
			panic(err)
		}

		// Current nodes
		stakedPeers := make(map[string]PeerNode)
		var totalStake uint64 = 0

		for _, node := range stakedNodes.Current {
			totalStake += node.ActivatedStake

			// Don't add my self and don't add unstaked nodes
			if *flagPubkey != "" {
				if node.NodePubkey.String() == *flagPubkey {
					continue
				}
			}
			if node.ActivatedStake < 0 {
				continue
			}

			stakedPeers[node.NodePubkey.String()] = PeerNode{
				Stake:  node.ActivatedStake,
				Pubkey: node.NodePubkey,
			}
		}

		// Delinquent nodes
		for _, node := range stakedNodes.Delinquent {
			totalStake += node.ActivatedStake

			// Don't add my self and don't add unstaked nodes
			if *flagPubkey != "" {
				if node.NodePubkey.String() == *flagPubkey {
					continue
				}
			}
			if node.ActivatedStake < 0 {
				continue
			}

			stakedPeers[node.NodePubkey.String()] = PeerNode{
				Stake:  node.ActivatedStake,
				Pubkey: node.NodePubkey,
			}
		}

		// Fetch the IPs for all teh cluster nodes
		nodes, err := client.GetClusterNodes(
			context.TODO(),
		)

		if err != nil {
			panic(err)
		}

		for _, node := range nodes {
			if *flagPubkey != "" {
				if *flagPubkey == node.Pubkey.String() {
					// If this is our node, configure the TPU forwarding rules
					if node.TPU != nil {
						tpu := strings.Split(*(node.TPU), ":")
						myTPUPort = tpu[1]
						ipt.AppendUnique("mangle", "PREROUTING", "-p", "udp", "--dport", myTPUPort, "-j", mangleChain)
						if err != nil {
							log.Println("couldn't add mangle rule for TPU", err)
						}
						err = ipt.AppendUnique("filter", "INPUT", "-p", "udp", "--dport", myTPUPort, "-j", filterChain)
						if err != nil {
							log.Println("couldn't add filter rule for TPU: ", err)
						}
					}
					// We don't add our own node to any classes
					continue
				}
			}

			// If the node has a gossip address
			if node.Gossip != nil {
				// Currently add both TPU and Gossip addresses if both are listed
				// not sure if TPU would ever be different from gossip (assumption: not)
				var addresses []string
				var gossip, tpu []string
				gossip = strings.Split(*(node.Gossip), ":")

				if node.TPU != nil {
					tpu = strings.Split(*(node.TPU), ":")
				}
				if len(gossip) > 0 {
					addresses = append(addresses, gossip[0])
				}
				if len(tpu) > 0 {
					if len(gossip) > 0 {
						if tpu[0] != gossip[0] {
							addresses = append(addresses, tpu[0])
						}
					} else {
						addresses = append(addresses, tpu[0])
					}
				}

				// If this is a staked node i.e. listed in staked peers
				if val, ok := stakedPeers[node.Pubkey.String()]; ok {
					percent := float64(val.Stake) / float64(totalStake)

					added := false
					for _, class := range cfg.Classes {
						// Add to the highest class it matches
						for _, addr := range addresses {
							if percent > class.Stake && !added {
								// Add to the first class found, then set flag
								// so we don't add it to any lower staked classes
								ipset.Add(class.Name, addr)
								added = true
							} else {
								// Delete from all other clasess
								ipset.Del(class.Name, addr)
							}
						}
					}
				} else {
					// unstaked node add to the special unstaked class
					// and delete from all other classes
					for _, addr := range addresses {
						ipset.Add(cfg.UnstakedClass.Name, addr)
						for _, class := range cfg.Classes {
							if class.Name != cfg.UnstakedClass.Name {
								ipset.Del(class.Name, addr)
							}
						}
					}
				}
			} else {
				fmt.Println("not visible in gossip", node.Pubkey.String())
			}
		}

		log.Println("Updated ipsets: ", len(nodes), " visible in gossip and added to ipset")

		// update every 10 secs
		time.Sleep(10 * time.Second)
	}
}
