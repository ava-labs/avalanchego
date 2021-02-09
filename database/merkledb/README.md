#Summary

##Tree

The Tree is a merkle radix trie implementation.

It takes a Persistence layer and a RootNode and operates seemingly to behave like a data structure.

##Forest

The Forest is a collection of Trees. 

It allows to manage multiple trees by manipulating the Persistence layer.

##Persistence layer

The Persistence layer is an interface that allows for different behaviours when CRUD
data. Depending on the persistence it allows to have a Single Tree or a Multi-Root Tree (Forest).

##RootNode

The Root Node is an implementation of a Node that is specialized in manipulating the entry point of a Tree.

It will always exist at the top of the Tree.

##LeafNode

The Leaf Node is an implementation of a Node that is specialized in storing values. 

It will always exist at the leaves of the Tree.

##BranchNode 
  
The Branch Node is an implementation of a Node that is specialized in storing children.

Everytime two Nodes try to occupy the same position that position will be replaced by a BranchNode holding both children. 

##Unit/Key

The Unit or Key are abstractions that help us deal with the underlying byte operations.


#Design choices

##No values on Branch Nodes

Branch Nodes hold the array of the Hashes of its children.

Having the Hashes immediately helps to compute the BranchNode Hash instead of fetching them from the children.

Deferring values to Children simplifies the logic of behaviour for the BranchNode.

##No Extension Branches

Eliminating the Extension Branch allows to simplify the logic by only having LeafNodes and BranchNodes.

##16 + 1 Positions on the Branch Node

There are 16 possible positions in the BranchNode array for its children.

The BranchNode is ID'd by its SharedAddress which allows to identify the prefix of its children.

Having variable length keys means that a LeafNode ID might collide with the BranchNode SharedAddress - 
in that unique situation the BranchNode uses the 17th position to store the given LeafNode Hash.



