const authorBio = ({parent: {name, dob}}) => `My name is ${name} and I was born on ${dob}.`
const characterBio = ({parent: {name}}) => `My name is ${name}.`
const humanBio = ({parent: {name, totalCredits}}) => `My name is ${name}. I have ${totalCredits} credits.`
const droidBio = ({parent: {name, primaryFunction}}) => `My name is ${name}. My primary function is ${primaryFunction}.`

async function authorsByName({args, dql}) {
    const results = await dql.query(`{
        queryAuthor(func: type(test.dgraph.author)) @filter(eq(test.dgraph.author.name, "${args.name}")) {
            name: test.dgraph.author.name
            dob: test.dgraph.author.dob
            reputation: test.dgraph.author.reputation
        }
    }`)
    return results.data.queryAuthor
}

async function newAuthor({args, graphql}) {
    // lets give every new author a reputation of 3 by default
    const results = await graphql(`mutation {
        addAuthor(input: [{name: "${args.name}", reputation: 3.0 }]) {
            author {
                id
                reputation
            }
        }
    }`)
    return results.data.addAuthor.author[0].id
}

self.addGraphQLResolvers({
    "Author.bio": authorBio,
    "Character.bio": characterBio,
    "Human.bio": humanBio,
    "Droid.bio": droidBio,
    "Query.authorsByName": authorsByName,
    "Mutation.newAuthor": newAuthor
})

async function rank({parents}) {
    const idRepList = parents.map(function (parent) {
        return {id: parent.id, rep: parent.reputation}
    });
    const idRepMap = {};
    idRepList.sort((a, b) => a.rep > b.rep ? -1 : 1)
        .forEach((a, i) => idRepMap[a.id] = i + 1)
    return parents.map(p => idRepMap[p.id])
}

self.addMultiParentGraphQLResolvers({
    "Author.rank": rank
})