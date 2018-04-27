# [TTK4145-Sanntidsprogrammering](https://www.ntnu.no/studier/emner/TTK4145)
TTK4145 (Real-time Programming)
## Elevator Project
![](https://raw.github.com/klasbo/TTK4145/master/Project/ElevatorHardware.jpg)

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

### [Simulator](https://github.com/TTK4145/Simulator-v2)
This simulator is a drop-in alternative to the elevator hardware server that interfaces to the hardware at the lab. The server is intended to run in its own window, as it also takes keyboard input to simulate button presses.

#### Configuration options

The simulator has several configuration options, which you can find [listed here](https://github.com/KristofferHegg/TTK4145-Sanntid/blob/master/Simulator/simulator.con). The most relevant options are:
 - `--port`: This is the TCP port used to connect to the simulator, which defaults to 15657.    
 You can start multiple simulators with different port numbers to run multiple elevators on a single machine.
 
Options passed on the command line (eg. `./SimElevatorServer --port 15658`) override the options in the the `simulator.con` config file, which in turn override the defaults baked in to the program.

#### Keyboard controls

 - Up: `qwe`
 - Down: `sdf`
 - Cab: `zxcv`
 - Stop: `p`
 - Obstruction: `-`
 - Move elevator back in bounds (away from the end stop switches): `0`

#### Display 
```
+-----------+-----------------+
|           |        #>       |
| Floor     |  0   1*  2   3  |Connected
+-----------+-----------------+-----------+
| Hall Up   |  *   -   -      | Door:   - |
| Hall Down |      -   -   *  | Stop:   - |
| Cab       |  -   -   *   -  | Obstr:  ^ |
+-----------+-----------------+---------43+
```
The ascii-art-style display is updated whenever the state of the simulated elevator is updated.

### Getting started
Comming Soon

### Credit
Thanks to [Kjetil Kjeka](https://github.com/kjetilkjeka) and [klasbo](https://github.com/klasbo) for the (awesome) handed out packages and simulator. 


