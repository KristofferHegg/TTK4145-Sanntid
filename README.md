# [TTK4145-Sanntidsprogrammering](https://www.ntnu.no/studier/emner/TTK4145)
TTK4145 (Real-time Programming)
## Elevator Project

### Description
In this project, we had to create software for controlling n elevators working in parallel across m floors. There were some main requirments, here summarised in bullet points:

* A placed order should be ignored if there is no way to assure redundancy.
* When a hall order is accepted, it must be served within reasonable time.
* The panel lights must be synchronized between elevators whenever it is reasonable to do so.
* Multiple elevators must be more efficient than one.
* The elevators should have a sense of direction, more specifically, the elevators must avoid serving hall-up and hall-down orders on the same floor at the same time.

### Our Solution
We wrote our soloution in GO. This project uses the (awesome) handed out packages: https://github.com/TTK4145/driver-go and https://github.com/TTK4145/Network-go.

We decided to use a "fleeting master" together with UDP broadcasting. All elevators knows about all other elevators state, direction, floor and orders. The elevator that receives an external order, will be the one to decide which elevator should execute the order. This decision, along with the order, is broadcasted to all other elevators on the network. The order is then acknowledged between the elevators before the panel light is lit. If an elevator lost network or failed to finish the order in a certain time, the other elevators would take over the order. If an elevator is operating normally, only without network, it functioned as a locally run elevator.
