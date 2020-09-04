import math
import matplotlib.pyplot as plt

maxAmount = 720000000
startingAmount = 1000000
startingTotalAmount = 360000000
maxSubMinConsumptionRate = 0.02
minConsumptionRate = 0.1


def reward(duration, stakedAmount, maxExistingAmount):
    adjustedConsumptionRate = maxSubMinConsumptionRate * duration + minConsumptionRate
    amountToMintInInterval = (maxAmount - maxExistingAmount) * adjustedConsumptionRate
    maxReward = amountToMintInInterval * stakedAmount / maxExistingAmount
    reward = maxReward * duration

    return reward


def correctReward(duration, stakedAmount, maxExistingAmount):
    adjustedConsumptionRate = maxSubMinConsumptionRate * duration + minConsumptionRate
    amountToMintInInterval = (maxAmount - maxExistingAmount) * adjustedConsumptionRate
    maxReward = amountToMintInInterval * stakedAmount / maxExistingAmount
    interestRate = maxReward / stakedAmount
    reward = (((1 + interestRate) ** duration) - 1) * stakedAmount

    return reward


def stake(numPeriods):
    amount = startingAmount
    for i in range(0, numPeriods):
        maxExistingAmount = startingTotalAmount - startingAmount + amount
        amount += reward(1 / numPeriods, amount, maxExistingAmount)
    return amount


def correctStake(numPeriods):
    amount = 1000000
    for i in range(0, numPeriods):
        maxExistingAmount = startingTotalAmount - startingAmount + amount
        amount += correctReward(1 / numPeriods, amount, maxExistingAmount)
    return amount


def main():
    rewardAmount = []
    correctRewardAmount = []
    for i in range(1, 26):
        rewardAmount.append(stake(i))
        correctRewardAmount.append(correctStake(i))

    plt.plot(range(len(rewardAmount)), rewardAmount)
    plt.plot(range(len(correctRewardAmount)), correctRewardAmount)
    plt.show()


if __name__ == "__main__":
    main()
