# cviz

Visualization tool, designed for image classification.
It generates HTML from JSON and opens a web browser. No 3rd party software
required.

![preview](./media/cviz.png)


## Install

```sh
go install github.com/piotrpersona/cviz
```

## Usage

```sh
cviz input.json
```

JSON must be in format:
```json
{
    "classes": [
        "dog",
        "cat",
        "bird"
    ],
    "objects": [
        {
            "filePath": "/path/to/dog.jpeg",
            "class": 0,
            "scores": [
                0.97,
                0.02,
                0.01
            ]
        }
    ]
}
```

Example in [./examples](./examples).

